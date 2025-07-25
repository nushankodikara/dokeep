from fastapi import FastAPI, UploadFile, File, Body, Form
from fastapi.responses import Response
from PIL import Image
from pdf2image import convert_from_bytes
import pytesseract
import io
import os
import joblib
import spacy
import time
import datefinder
import logging
import hashlib
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.naive_bayes import MultinomialNB
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import MultiLabelBinarizer
from sklearn.multiclass import OneVsRestClassifier
import threading
from contextlib import asynccontextmanager
import sqlite3
import requests
import psycopg2
from psycopg2.extras import DictCursor
from psycopg2 import errors

app = FastAPI()

# --- LLM Service Communication ---
LLM_SERVICE_URL = os.getenv("LLM_SERVICE_URL", "http://llm-service:8001/analyze")

def call_llm_service(content: str) -> dict:
    """
    Calls the LLM service to get summary, tags, and extracted date.
    """
    if os.getenv("DISABLE_AI") == "1":
        logging.info("AI features are disabled. Skipping LLM service call.")
        return {}
    
    try:
        response = requests.post(
            LLM_SERVICE_URL,
            json={"content": content, "initial_tags": []}, # We are not using initial tags for now
            timeout=600 # 10 minute timeout
        )
        response.raise_for_status()
        return response.json()
    except requests.exceptions.RequestException as e:
        logging.error(f"Error calling LLM service: {e}")
        return {}

# --- Database Connection ---
def get_db_connection():
    try:
        conn = psycopg2.connect(
            host=os.getenv("DB_HOST"),
            dbname=os.getenv("DB_NAME"),
            user=os.getenv("DB_USER"),
            password=os.getenv("DB_PASSWORD")
        )
        return conn
    except psycopg2.OperationalError as e:
        logging.error(f"Could not connect to PostgreSQL database: {e}")
        return None

def update_document_status(doc_id: int, status: str, message: str = ""):
    conn = get_db_connection()
    if conn is None:
        return
        
    try:
        with conn.cursor() as cursor:
            cursor.execute(
                "UPDATE documents SET status = %s, status_message = %s WHERE id = %s",
                (status, message, doc_id)
            )
            conn.commit()
        logging.info(f"Updated document {doc_id} status to '{status}'")
    except Exception as e:
        logging.error(f"Failed to update status for document {doc_id}: {e}")
    finally:
        if conn:
            conn.close()

def update_document_with_results(doc_id: int, result: dict):
    """
    Updates the document record with the analysis results. It first checks for
    duplicates by saving the file hash, and only proceeds to expensive AI analysis
    if the document is unique.
    """
    conn = get_db_connection()
    if conn is None:
        return

    file_hash = result.get("file_hash")
    if not file_hash:
        update_document_status(doc_id, "failed", "Could not calculate file hash.")
        return

    try:
        # Phase 1: Check for duplicates by saving the hash first.
        with conn.cursor() as cursor:
            cursor.execute("UPDATE documents SET file_hash = %s WHERE id = %s", (file_hash, doc_id))
            conn.commit()
        logging.info(f"Successfully saved file hash for document {doc_id}.")

    except errors.UniqueViolation as e:
        # This is a duplicate file, so we clean it up completely and stop.
        logging.warning(f"Duplicate document detected for doc_id {doc_id} based on file hash. Cleaning up.")
        cleanup_failed_document(doc_id, result.get("thumbnail_path"))
        if conn:
            conn.close()
        return  # Stop processing
    except Exception as e:
        logging.error(f"Failed to save file_hash for document {doc_id}: {e}")
        update_document_status(doc_id, "failed", f"Error saving file hash to database: {e}")
        if conn:
            conn.close()
        return  # Stop processing

    # If we reach here, the document is unique.
    # Phase 2: Perform expensive analysis and save the final results.
    try:
        with conn.cursor() as cursor:
            # Get final analysis from LLM
            llm_result = call_llm_service(result.get("text", ""))

            # Combine results
            final_summary = llm_result.get("summary", "")
            final_tags = llm_result.get("tags", [])

            # Use LLM date if available, otherwise fall back to OCR date
            final_date_str = llm_result.get("extracted_date")
            if not final_date_str:
                final_date_str = result.get("extracted_date")

            cursor.execute(
                """
                UPDATE documents
                SET content = %s, thumbnail = %s, summary = %s, created_date = %s, status = 'completed'
                WHERE id = %s
                """,
                (
                    result.get("text"),
                    result.get("thumbnail_path"),
                    final_summary,
                    final_date_str,  # Store as string, Go app will parse
                    doc_id,
                ),
            )
            conn.commit()

            # Add tags to the document
            add_tags_to_document(doc_id, final_tags)

        logging.info(f"Successfully saved all analysis results for document {doc_id}")
    except Exception as e:
        logging.error(f"Failed to save final results for document {doc_id}: {e}")
        update_document_status(doc_id, "failed", f"Error during final analysis and save: {e}")
    finally:
        if conn:
            conn.close()


def cleanup_failed_document(doc_id: int, thumbnail_path_from_processing: str = None):
    """
    Deletes the document record and associated files for a failed upload,
    typically used for duplicates.
    """
    logging.info(f"Initiating cleanup for failed document ID: {doc_id}")
    conn = get_db_connection()
    if conn is None:
        logging.error(f"Cleanup failed for doc {doc_id}: could not get DB connection.")
        return

    try:
        with conn.cursor(cursor_factory=DictCursor) as cursor:
            # 1. Get file path before deleting the record
            cursor.execute("SELECT file_path, thumbnail FROM documents WHERE id = %s", (doc_id,))
            record = cursor.fetchone()
            if not record:
                logging.warning(f"Cleanup for doc {doc_id}: Record already gone.")
                # Still try to clean up the thumbnail if we have its path
                if thumbnail_path_from_processing and os.path.exists(thumbnail_path_from_processing):
                    os.remove(thumbnail_path_from_processing)
                    logging.info(f"Deleted orphaned thumbnail: {thumbnail_path_from_processing}")
                return

            file_path = record["file_path"]

            # 2. Delete the document record from the database
            cursor.execute("DELETE FROM documents WHERE id = %s", (doc_id,))
            conn.commit()
            logging.info(f"Deleted document record for ID: {doc_id}")

            # 3. Delete the files from the filesystem
            if file_path and os.path.exists(file_path):
                os.remove(file_path)
                logging.info(f"Deleted file: {file_path}")

            # Use the path passed from the processing step for the thumbnail, as it's not in the DB yet.
            if thumbnail_path_from_processing and os.path.exists(thumbnail_path_from_processing):
                os.remove(thumbnail_path_from_processing)
                logging.info(f"Deleted thumbnail: {thumbnail_path_from_processing}")

    except Exception as e:
        logging.error(f"An error occurred during cleanup for document {doc_id}: {e}")
    finally:
        if conn:
            conn.close()


def add_tags_to_document(doc_id: int, tags: list):
    """
    Adds tags to a document, creating them if they don't exist.
    """
    if not tags:
        return
        
    conn = get_db_connection()
    if conn is None:
        return
        
    try:
        with conn.cursor(cursor_factory=DictCursor) as cursor:
            for tag_name in tags:
                normalized_tag = tag_name.strip().lower()
                if not normalized_tag:
                    continue
                
                # Find or create the tag
                cursor.execute("SELECT id FROM tags WHERE name = %s", (normalized_tag,))
                tag_row = cursor.fetchone()
                if tag_row:
                    tag_id = tag_row['id']
                else:
                    cursor.execute("INSERT INTO tags (name) VALUES (%s) RETURNING id", (normalized_tag,))
                    tag_id = cursor.fetchone()['id']
                
                # Associate tag with document
                cursor.execute("INSERT INTO document_tags (document_id, tag_id) VALUES (%s, %s) ON CONFLICT DO NOTHING", (doc_id, tag_id))
                
            conn.commit()
    except Exception as e:
        logging.error(f"Failed to add tags for document {doc_id}: {e}")
    finally:
        if conn:
            conn.close()

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# Load SpaCy model
nlp = spacy.load("en_core_web_sm")

def worker():
    """
    The background worker that processes files from the queue.
    """
    queue_dir = "uploads/queue"
    logging.info("Background worker started.")
    while True:
        try:
            files_in_queue = [f for f in os.listdir(queue_dir) if os.path.isfile(os.path.join(queue_dir, f))]
            for filename in files_in_queue:
                file_path = os.path.join(queue_dir, filename)
                
                # The filename is the document ID
                try:
                    doc_id = int(os.path.splitext(filename)[0])
                except ValueError:
                    logging.error(f"Invalid filename in queue (should be an integer ID): {filename}. Deleting.")
                    os.remove(file_path)
                    continue

                logging.info(f"Worker picked up document ID: {doc_id}")
                
                update_document_status(doc_id, "processing")
                
                result = _process_document_task(file_path)
                
                if result:
                    logging.info(f"Successfully processed document ID: {doc_id}.")
                    update_document_with_results(doc_id, result)
                else:
                    logging.error(f"Failed to process document ID: {doc_id}.")
                    update_document_status(doc_id, "failed", "An unexpected error occurred during processing.")

                # Clean up the file from the queue
                os.remove(file_path)
                logging.info(f"Removed {filename} from queue.")

        except FileNotFoundError:
            # This is expected if the queue directory doesn't exist yet.
            pass
        except Exception as e:
            logging.error(f"An error occurred in the worker loop: {e}")
        
        time.sleep(5) # Wait for 5 seconds before checking the queue again

@asynccontextmanager
async def lifespan(app: FastAPI):
    # Start the background worker
    worker_thread = threading.Thread(target=worker, daemon=True)
    worker_thread.start()
    yield
    # Clean up the thread if needed, although daemon=True should handle it

app = FastAPI(lifespan=lifespan)


# In-memory storage for the model and binarizer
model_pipeline = None
mlb = None

def preprocess_text(text):
    doc = nlp(text.lower())
    lemmas = [token.lemma_ for token in doc if not token.is_stop and not token.is_punct and token.is_alpha]
    return " ".join(lemmas)

def load_model():
    global model_pipeline, mlb
    if os.path.exists("tag_model.joblib"):
        model_pipeline, mlb = joblib.load("tag_model.joblib")

load_model()

@app.post("/thumbnail")
async def create_thumbnail(file: UploadFile = File(...)):
    contents = await file.read()
    
    _, ext = os.path.splitext(file.filename)
    ext = ext.lower()

    if ext == ".pdf":
        images = convert_from_bytes(contents, first_page=1, last_page=1, fmt="jpeg")
        if images:
            img = images[0]
            img.thumbnail((500, 500))
            buf = io.BytesIO()
            img.save(buf, format='JPEG')
            return Response(buf.getvalue(), media_type="image/jpeg")
    elif ext in [".jpg", ".jpeg", ".png"]:
        img = Image.open(io.BytesIO(contents))
        img.thumbnail((100, 100))
        buf = io.BytesIO()
        img.save(buf, format='JPEG')
        return Response(buf.getvalue(), media_type="image/jpeg")
        
    return {"error": "Unsupported file type"}


@app.post("/process")
async def process_document(doc_id: int = Form(...), file: UploadFile = File(...)):
    # This endpoint now simply saves the file to a queue directory.
    # The background worker will process it.
    queue_dir = "uploads/queue"
    os.makedirs(queue_dir, exist_ok=True)

    # The filename in the queue is the document ID
    _, ext = os.path.splitext(file.filename)
    new_filename = f"{doc_id}{ext}"
    file_path = os.path.join(queue_dir, new_filename)
    
    with open(file_path, "wb") as f:
        contents = await file.read()
        f.write(contents)

    logging.info(f"File for doc ID '{doc_id}' saved to queue for processing as '{new_filename}'.")
    return {"status": "queued", "doc_id": doc_id}


def _process_document_task(file_path: str):
    """
    This function contains the core logic for processing a single document.
    It's designed to be called by the background worker.
    """
    filename = os.path.basename(file_path)
    logging.info(f"Worker processing document: {filename}")

    with open(file_path, "rb") as f:
        contents = f.read()

    # Calculate SHA256 hash
    file_hash = hashlib.sha256(contents).hexdigest()
    logging.info(f"Calculated SHA256 hash for {filename}: {file_hash}")

    # --- Paths inside the container ---
    thumb_dir = "uploads/thumbnails"
    os.makedirs(thumb_dir, exist_ok=True)
    
    base_name = os.path.splitext(filename)[0]
    unique_thumb_filename = f"{base_name}_{int(time.time())}.jpg"
    
    thumbnail_save_path = os.path.join(thumb_dir, unique_thumb_filename)
    thumbnail_return_path = os.path.join("uploads", "thumbnails", unique_thumb_filename)

    ocr_text = ""
    ext = os.path.splitext(filename)[1].lower()

    try:
        if ext == ".pdf":
            images = convert_from_bytes(contents, fmt="jpeg")
            if images:
                for img in images:
                    ocr_text += pytesseract.image_to_string(img) + "\n"
                
                first_page_img = images[0]
                first_page_img.thumbnail((500, 500))
                first_page_img.save(thumbnail_save_path, format='JPEG')
            else:
                thumbnail_return_path = ""
                
        elif ext in [".jpg", ".jpeg", ".png"]:
            img = Image.open(io.BytesIO(contents))
            ocr_text = pytesseract.image_to_string(img)
            img.thumbnail((100, 100))
            img.save(thumbnail_save_path, format='JPEG')
        else:
            thumbnail_return_path = "" # Unsupported type

        logging.info(f"Extracted {len(ocr_text)} characters from {filename}")

        # Date extraction
        doc = nlp(ocr_text)
        extracted_date = None
        for ent in doc.ents:
            if ent.label_ == "DATE":
                found_dates = list(datefinder.find_dates(ent.text))
                if found_dates:
                    extracted_date = found_dates[0].isoformat()
                    break

        if not extracted_date:
            found_dates = list(datefinder.find_dates(ocr_text))
            if found_dates:
                extracted_date = found_dates[0].isoformat()

        if extracted_date:
            logging.info(f"Found extracted date for {filename}: {extracted_date}")

        return {
            "text": ocr_text,
            "thumbnail_path": thumbnail_return_path,
            "extracted_date": extracted_date,
            "file_hash": file_hash,
        }
    except Exception as e:
        logging.error(f"Error processing {filename}: {e}")
        # Here, we would update the database with a 'failed' status
        return None


@app.post("/ocr")
async def ocr(file: UploadFile = File(...)):
    contents = await file.read()
    
    _, ext = os.path.splitext(file.filename)
    ext = ext.lower()

    if ext == ".pdf":
        images = convert_from_bytes(contents, fmt="jpeg")
        text = ""
        for img in images:
            text += pytesseract.image_to_string(img) + "\n"
        return {"text": text}
    elif ext in [".jpg", ".jpeg", ".png"]:
        text = pytesseract.image_to_string(Image.open(io.BytesIO(contents)))
        return {"text": text}

    return {"text": ""}


@app.post("/train")
async def train(data: dict = Body(...)):
    global model_pipeline, mlb
    documents = data.get("documents", [])
    tags = data.get("tags", [])

    processed_docs = [preprocess_text(doc) for doc in documents]

    mlb = MultiLabelBinarizer()
    y = mlb.fit_transform(tags)

    model_pipeline = Pipeline([
        ('tfidf', TfidfVectorizer()),
        ('clf', OneVsRestClassifier(MultinomialNB()))
    ])
    
    model_pipeline.fit(processed_docs, y)

    joblib.dump((model_pipeline, mlb), "tag_model.joblib")
    return {"status": "Training complete"}

@app.post("/predict")
async def predict(data: dict = Body(...)):
    if model_pipeline is None or mlb is None:
        return {"tags": []}

    document = data.get("document", "")
    processed_doc = preprocess_text(document)
    
    predicted_binares = model_pipeline.predict([processed_doc])
    predicted_tags = mlb.inverse_transform(predicted_binares)
    
    return {"tags": predicted_tags[0] if predicted_tags else []} 