---
name: office-parse
description: Extract text locally from common document files such as txt, md, csv, tsv, docx, pptx, and xlsx. Use when the user asks what a local document, spreadsheet, or slide deck says and you should parse it without sending the file to an external API or LLM.
metadata: {"nanobot":{"emoji":"🗂️","requires":{"bins":["python3"]}}}
---

# Office Parse

Use this skill to extract text or sheet content from common local documents without calling any external service.

## Workflow

1. Resolve the local file path.
2. Run the helper:

```bash
python3 workspace/skills/office-parse/scripts/extract_office.py "/path/to/file.docx"
```

On Windows, use `py -3` if `python3` is unavailable.

3. Use the extracted output to answer directly.

## Supported files

- `.txt`
- `.md`
- `.csv`
- `.tsv`
- `.docx`
- `.pptx`
- `.xlsx`

## Output rules

- The helper prints local extracted content only.
- Do not send the file to an external summarizer.
- For spreadsheets, use the extracted sheet names and row data in your answer.
- If a file is truncated, malformed, or partly unreadable, say that clearly.
- Keep any user caption or question as the primary intent signal.
