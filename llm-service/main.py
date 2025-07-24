import ollama
from fastapi import FastAPI, Body
import logging
from pydantic import BaseModel
import datetime
from typing import Optional, List

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

app = FastAPI()

class AnalysisRequest(BaseModel):
    content: str

class AnalysisResponse(BaseModel):
    extracted_date: Optional[datetime.datetime] = None
    tags: List[str] = []

@app.on_event("startup")
async def startup_event():
    logging.info("Pulling the qwen2:0.5b model...")
    try:
        ollama.pull('qwen2:0.5b')
        logging.info("Model pulled successfully.")
    except Exception as e:
        logging.error(f"Failed to pull model: {e}")

@app.post("/analyze", response_model=AnalysisResponse)
async def analyze_content(request: AnalysisRequest):
    logging.info("Received request for content analysis.")
    prompt = f"""
    Analyze the following document content. Your task is to extract two pieces of information:
    1. The creation date of the document. If a clear date is present, return it in YYYY-MM-DD format.
    2. A list of 3 to 5 relevant tags that categorize the document. Return these as a comma-separated list.

    Respond with ONLY a JSON object with two keys: "extracted_date" and "tags".
    Example response: {{"extracted_date": "2023-01-15", "tags": "finance, report, quarterly"}}

    Document Content:
    ---
    {request.content}
    ---
    """
    try:
        response = ollama.generate(model='qwen2:0.5b', prompt=prompt, format='json')
        
        # The response from ollama.generate is a dict, and the actual content is in the 'response' key
        # We need to parse this string to get the JSON object.
        import json
        result = json.loads(response['response'])
        
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

        logging.info(f"Analysis complete. Found date: {extracted_date}, Found tags: {tags}")
        return AnalysisResponse(extracted_date=extracted_date, tags=tags)
        
    except Exception as e:
        logging.error(f"Error during LLM analysis: {e}")
        return AnalysisResponse() 