import os
from dotenv import load_dotenv
import ollama
import openai
from fastapi import FastAPI, Body
import logging
from pydantic import BaseModel
import datetime
from typing import Optional, List

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
    initial_tags: List[str] = []

class AnalysisResponse(BaseModel):
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
    prompt = f"""
    You are an expert document analyst. Your task is to analyze the following document content and a list of initial machine learning-generated tags. Your goal is to refine and improve this list and provide a concise summary.

    Based on the document content, please provide:
    1. The creation date of the document in YYYY-MM-DD format if a clear date is present.
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

        import json
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

        logging.info(f"Analysis complete. Found date: {extracted_date}, Found tags: {tags}, Found summary: {summary}")
        return AnalysisResponse(extracted_date=extracted_date, tags=tags, summary=summary)
        
    except Exception as e:
        logging.error(f"Error during LLM analysis: {e}")
        return AnalysisResponse() 