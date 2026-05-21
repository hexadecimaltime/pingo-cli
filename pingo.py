import requests
from bs4 import BeautifulSoup
import time, sys
import urllib.parse
import subprocess
import shutil
import re
from typing import cast

BASE_URL: str = "https://pingo.coactum.de"

NUMBER_INPUT_PATTERN = re.compile(r"^[+-]?\d+(?:\.\d+)?$")

def check_gum():
    if not shutil.which("gum"):
        print("Error: 'gum' is not installed.")
        print("Please install gum (https://github.com/charmbracelet/gum), with 'winget install charmbracelet.gum' on Windows")
        sys.exit(1)

"""Helper to run gum command and return stdout.
   Exits gracefully if user cancels.
"""
def run_gum(args):
    result: subprocess.CompletedProcess = subprocess.run(["gum"] + args, stdout=subprocess.PIPE, text=True)
    if result.returncode != 0:
        sys.exit(0)
    return result.stdout.strip()

def main():
    check_gum()
    
    print("\n")
    
    # Parse args or prompt for session code
    if len(sys.argv) > 1:
        session_code: str = sys.argv[1]
    else:
        session_code: str = run_gum(["input", "--placeholder", "Enter PINGO session code...", "--width", "40"])
        
    if not session_code:
        sys.exit(0)
        
    session_url: str = f"{BASE_URL}/{session_code}"
    http_session: requests.Session = requests.Session()
    
    subprocess.run(["gum", "style", "--italic", f"Connecting to session {session_code}..."])
    subprocess.run(["gum", "style", "--italic", "Waiting for an active poll or survey..."])
    
    form = None
    soup: BeautifulSoup | None = None
    
    # Polling for active poll/survey
    while not form:
        try:
            response: requests.Response = http_session.get(session_url)
            soup = BeautifulSoup(response.text, "html.parser")
        except requests.exceptions.RequestException:
            time.sleep(3)
            continue
            
        form = soup.find("form", action="/vote")
        
        if not form:
            survey_url: str | None = None
            # guard against soup = None for static checkers
            survey_links = soup.find_all("a", href=True) if soup is not None else []

            for link in survey_links:
                href = cast(str, link.get('href', ''))
                href_lower = href.lower()
                text_lower = link.get_text(strip=True).lower()

                if ("survey" in href_lower and "participate" in href_lower) or \
                   ("survey" in text_lower and "participate" in text_lower):
                    survey_url = urllib.parse.urljoin(BASE_URL, href)
                    break
                
            if survey_url:
                response: requests.Response = http_session.get(survey_url)
                soup = BeautifulSoup(response.text, "html.parser")
                form = soup.find("form")
                if form:
                    break
            
        if not form:
            time.sleep(3)
            
    # Get question
    subprocess.run(["gum", "style", "--border", "rounded", "--margin", "1 0", "--padding", "0 2", "--bold", "POLL ACTIVE!"])
    
    question_container = soup.find("div", class_="question-text") if soup is not None else None
    if question_container:
        question_text: str = question_container.get_text(strip=True)
        subprocess.run(["gum", "style", "--bold", f"Q: {question_text}"])
    else:
        subprocess.run(["gum", "style", "--bold", "Please follow the instructions below:"])
        
    # Hidden data
    hidden_inputs = form.find_all("input", type="hidden") if form is not None else []
    payload: dict = {}
    for input in hidden_inputs:
        if input.has_attr("name") and input.has_attr("value"):
            payload[input["name"]] = input["value"]
    
    # Detect inputs
    
    text_inputs = form.find_all("input", {"type": ["text", "number"]})
    text_areas = form.find_all("textarea")
    
    if text_areas or text_inputs:
        target_element = text_areas[0] if text_areas else text_inputs[0]
        raw_input_name = target_element.get("name", "input")
        input_name: str = raw_input_name if isinstance(raw_input_name, str) else "input"
        input_type: str = cast(str, target_element.get("type") or "")
    
        # Ensure not calling find on None and pass a str for the attrs mapping
        target_id: str = cast(str, target_element.get("id") or "")
        label = soup.find("label", attrs={"for": target_id}) if soup is not None else None
        label_text: str = label.get_text(strip=True) if label else "Enter your answer"

        page_text: str = soup.get_text().lower() if soup is not None else ""
        is_multiple_choice: bool = input_name.endswith("[]") or "multiple answers" in page_text or "mehrere antworten" in page_text
        is_number_input: bool = input_type == "number" or "please enter the number without a thousands delimiter" in page_text
        
        if is_multiple_choice:
            if not input_name.endswith("[]"):
                input_name += "[]"
                
            subprocess.run(["gum", "style", "--italic", "(Multiple answers allowed. Submit an empty field to finish.)"])
            user_answers: list[str] = []
            
            while True:
                answer = run_gum(["input", "--placeholder", f"Answer {len(user_answers) + 1}..."])
                if not answer:
                    break
                user_answers.append(answer)
            payload[input_name] = user_answers
            
        else:
            if is_number_input:
                while True:
                    user_answer = run_gum(["input", "--placeholder", label_text])
                    if NUMBER_INPUT_PATTERN.fullmatch(user_answer):
                        payload[input_name] = user_answer
                        break
                    subprocess.run(["gum", "style", "--foreground", "9", "Please enter a number without thousands delimiters, for example 42.7."])
            else:
                user_answer = run_gum(["input", "--placeholder", label_text])
                payload[input_name] = user_answer
            
    else:
        option_inputs = form.find_all("input", {"name": ["option", "option[]", "options[]", "survey_answer[]"]}) if form is not None else []
        options: list[dict] = []
        is_multiple_choice: bool = False
        input_name: str = "option"
        
        for idx, opt in enumerate(option_inputs):
            if opt.get("type") == "hidden":
                continue
            if opt.get("type") == "checkbox":
                is_multiple_choice = True
            input_name = cast(str, opt.get("name") or "option")
                
            opt_id: str = cast(str, opt.get("id") or "")
            label = soup.find("label", attrs={"for": opt_id}) if soup is not None else None
            label_text = label.get_text(strip=True) if label else f"Option {idx}"
            value_text = cast(str, opt.get("value") or "")
            options.append({"label": label_text, "value": value_text, "input": opt})
            
        if not options:
            subprocess.run(["gum", "style", "Error: Could not parse voting options."])
            sys.exit(1)
            
        if is_multiple_choice:
            header: str = "Select your answers (Space to select, Enter to confirm)"
            gum_args: list[str] = ["checkbox", "--header", header] + [o["label"] for o in options]
            selected_options: list[str] = run_gum(gum_args).split("\n")

            selected_values: list[str] = []
            for selected in selected_options:
                for o in options:
                    if o["label"] == selected:
                        selected_values.append(o["value"])
                        break
            payload[input_name] = selected_values
            
        else:
            header: str = "Select your answer (Arrow keys to move, Enter to confirm)"
            gum_args: list[str] = ["choose", "--header", header] + [o["label"] for o in options]
            selected_label: str = run_gum(gum_args)

            selected_value: str = next(o["value"] for o in options if o["label"] == selected_label)
            payload[input_name] = selected_value
            
    # Submit answer
    payload["commit"] = "Vote!"
    if "utf8" not in payload:
        payload["utf8"] = "✓"
        
    submit_path: str = cast(str, form.get("action") or "/vote")

    submit_url: str = urllib.parse.urljoin(BASE_URL, submit_path)

    submit_response: requests.Response = http_session.post(
        submit_url,
        data=payload,
        allow_redirects=False
    )
    
    print("\n")
    if submit_response.status_code in (302, 303):
        subprocess.run(["gum", "style", "--border", "normal", "--margin", "1", "--padding", "0 1", "Success! Answer submitted. ✓"])
    else:
        subprocess.run(["gum", "style", f"Failed to submit. Server returned Status Code: {submit_response.status_code}"])
        
        
if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nExiting...")
        sys.exit(0)
        