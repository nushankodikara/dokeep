import os
from dotenv import load_dotenv
import ollama
import openai
from fastapi import FastAPI, Body
import logging
from pydantic import BaseModel
import datetime
from typing import Optional, List
import json

# Load environment variables from .env file
load_dotenv()

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

app = FastAPI()

# Determine which LLM provider to use
LLM_PROVIDER = "ollama"
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY")
OPENAI_MODEL = os.getenv("OPENAI_MODEL", "gpt-4o")
OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "qwen2:0.5b")

if OPENAI_API_KEY:
    LLM_PROVIDER = "openai"
    openai.api_key = OPENAI_API_KEY
    logging.info(f"Using OpenAI as the LLM provider with model {OPENAI_MODEL}.")
else:
    logging.info(f"Using Ollama as the LLM provider with model {OLLAMA_MODEL}.")

class AnalysisRequest(BaseModel):
    content: str
    filename: Optional[str] = None
    initial_tags: List[str] = []

class AnalysisResponse(BaseModel):
    title: Optional[str] = None
    extracted_date: Optional[datetime.datetime] = None
    tags: List[str] = []
    summary: Optional[str] = None

@app.on_event("startup")
async def startup_event():
    if LLM_PROVIDER == "ollama":
        logging.info(f"Pulling the {OLLAMA_MODEL} model for Ollama...")
        try:
            ollama.pull(OLLAMA_MODEL)
            logging.info("Model pulled successfully.")
        except Exception as e:
            logging.error(f"Failed to pull Ollama model: {e}")

@app.post("/analyze", response_model=AnalysisResponse)
async def analyze_content(request: AnalysisRequest):
    logging.info("Received request for content analysis.")

    # Base prompt
    prompt_lines = [
        "You are an expert document analyst. Your task is to analyze the following document content."
    ]
    
    # JSON keys to expect in the response
    json_keys = ["extracted_date", "tags", "summary"]
    
    # Instructions list
    instructions = [
        "1. The creation date of the document in YYYY-MM-DD format if a clear date is present.",
        "2. A final, refined list of 3 to 5 relevant tags.",
        "3. A concise, one or two-sentence summary of the document."
    ]

    # Conditionally add title generation to the prompt
    if request.filename:
        prompt_lines.append(f"The original filename is '{request.filename}'. Use this and the content to generate a short, descriptive title for the document.")
        instructions.insert(0, "1. A short, descriptive title for the document.")
        json_keys.insert(0, "title")

    prompt_lines.append("Based on the document content, please provide:")
    prompt_lines.extend(instructions)
    prompt_lines.append(f"Respond with ONLY a JSON object with the keys: {json.dumps(json_keys)}.")
    
    prompt = "\n".join(prompt_lines) + f"""
    ---
    Document Content:
    ---
    {request.content}
    ---
    """

    try:
        if LLM_PROVIDER == "openai":
            client = openai.OpenAI()
            response = client.chat.completions.create(
                model=OPENAI_MODEL,
                messages=[{"role": "user", "content": prompt}],
                response_format={"type": "json_object"}
            )
            result_str = response.choices[0].message.content
        else: # ollama
            response = ollama.generate(model=OLLAMA_MODEL, prompt=prompt, format='json')
            result_str = response['response']

        result = json.loads(result_str)
        
        # Validate and format the date
        extracted_date = None
        if result.get("extracted_date"):
            try:
                extracted_date = datetime.datetime.strptime(result["extracted_date"], "%Y-%m-%d")
            except ValueError:
                logging.warning(f"LLM returned a date in an invalid format: {result['extracted_date']}")
                extracted_date = None

        tags = result.get("tags", [])
        if isinstance(tags, str):
            tags = [tag.strip() for tag in tags.split(',')]
        
        summary = result.get("summary", None)
        title = result.get("title", None)

        logging.info(f"Analysis complete. Found title: {title}, date: {extracted_date}, tags: {tags}, summary: {summary}")
        return AnalysisResponse(title=title, extracted_date=extracted_date, tags=tags, summary=summary)
        
    except Exception as e:
        logging.error(f"Error during LLM analysis: {e}")
        return AnalysisResponse() 