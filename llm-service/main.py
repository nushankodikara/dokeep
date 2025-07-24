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
    initial_tags: List[str] = []

class AnalysisResponse(BaseModel):
    extracted_date: Optional[datetime.datetime] = None
    tags: List[str] = []
    summary: Optional[str] = None

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
    You are an expert document analyst. Your task is to analyze the following document content and a list of initial machine learning-generated tags. Your goal is to refine and improve this list and provide a concise summary.

    Based on the document content, please provide:
    1. The creation date of the document in YYY-MM-DD format if a clear date is present.
    2. A final, refined list of 3 to 5 relevant tags.
    3. A concise, one or two-sentence summary of the document.

    Respond with ONLY a JSON object with three keys: "extracted_date", "tags", and "summary".
    Example response: {{"extracted_date": "2023-01-15", "tags": "finance, quarterly report, planning", "summary": "This document is a quarterly financial report detailing revenue and projections."}}

    Initial Tags: {", ".join(request.initial_tags)}
    ---
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
        
        summary = result.get("summary", None)

        logging.info(f"Analysis complete. Found date: {extracted_date}, Found tags: {tags}, Found summary: {summary}")
        return AnalysisResponse(extracted_date=extracted_date, tags=tags, summary=summary)
        
    except Exception as e:
        logging.error(f"Error during LLM analysis: {e}")
        return AnalysisResponse() 