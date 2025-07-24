from fastapi import FastAPI, UploadFile, File, Body
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
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.naive_bayes import MultinomialNB
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import MultiLabelBinarizer
from sklearn.multiclass import OneVsRestClassifier

app = FastAPI()

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# Load SpaCy model
nlp = spacy.load("en_core_web_sm")

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
async def process_document(file: UploadFile = File(...)):
    logging.info(f"Received document for processing: {file.filename}")
    contents = await file.read()
    filename = file.filename
    
    # --- Paths inside the container ---
    # The volume mounts './uploads' from the host to '/app/uploads' in the container
    thumb_dir = "uploads/thumbnails"
    os.makedirs(thumb_dir, exist_ok=True)
    
    # Create a unique name to avoid file collisions
    base_name = os.path.splitext(filename)[0]
    unique_filename = f"{base_name}_{int(time.time())}.jpg"
    
    # This is the actual path where the file will be saved inside the container
    thumbnail_save_path = os.path.join(thumb_dir, unique_filename)
    # This is the path the Go app will use to serve the file
    thumbnail_return_path = os.path.join("uploads", "thumbnails", unique_filename)

    ocr_text = ""
    ext = os.path.splitext(filename)[1].lower()

    if ext == ".pdf":
        # Convert PDF to images once for both OCR and thumbnail
        images = convert_from_bytes(contents, fmt="jpeg")
        if images:
            # OCR from all pages
            for img in images:
                ocr_text += pytesseract.image_to_string(img) + "\n"
            
            # Thumbnail from the first page
            first_page_img = images[0]
            first_page_img.thumbnail((500, 500))
            first_page_img.save(thumbnail_save_path, format='JPEG')
        else:
            thumbnail_return_path = "" # No image, no thumbnail
            
    elif ext in [".jpg", ".jpeg", ".png"]:
        img = Image.open(io.BytesIO(contents))
        
        # OCR
        ocr_text = pytesseract.image_to_string(img)
        
        # Thumbnail
        img.thumbnail((100, 100))
        img.save(thumbnail_save_path, format='JPEG')
    else:
        thumbnail_return_path = "" # Unsupported type

    logging.info(f"Extracted {len(ocr_text)} characters from {filename}")

    # First, try to find a high-confidence date using SpaCy's NER
    doc = nlp(ocr_text)
    extracted_date = None
    for ent in doc.ents:
        if ent.label_ == "DATE":
            # Found a date entity, now try to parse it.
            found_dates = list(datefinder.find_dates(ent.text))
            if found_dates:
                extracted_date = found_dates[0].isoformat()
                break # Stop after finding the first valid date

    # If the NLP model didn't find a date, fall back to a broader search
    if not extracted_date:
        found_dates = list(datefinder.find_dates(ocr_text))
        if found_dates:
            extracted_date = found_dates[0].isoformat()

    if extracted_date:
        logging.info(f"Found extracted date for {filename}: {extracted_date}")
    else:
        logging.info(f"No date found for {filename}")

    logging.info(f"Finished processing {filename}. Thumbnail at: {thumbnail_return_path}")
    return {"text": ocr_text, "thumbnail_path": thumbnail_return_path, "extracted_date": extracted_date}


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