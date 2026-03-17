---
name: pdf-parse
description: Extract text locally from PDF files in the workspace or inbound_media directory. Use when the user asks what a PDF says, wants text pulled from a PDF, or wants a PDF parsed without sending the file to an external API or LLM.
metadata: {"nanobot":{"emoji":"📄","requires":{"bins":["python3"]}}}
---

# PDF Parse

Use this skill to extract text from a local PDF file without calling any external service.

## Workflow

1. Resolve the local PDF path.
2. Run the helper:

```bash
python3 workspace/skills/pdf-parse/scripts/extract_pdf.py "/path/to/file.pdf"
```

On Windows, use `py -3` if `python3` is unavailable.

3. Use the extracted text to answer directly.

## Input rules

- Local files only.
- Supported suffix: `.pdf`
- Text PDFs work best.
- Image-only or scanned PDFs will fail with a clear message instead of silently using a remote model.

## Output rules

- The helper prints plain text with page markers.
- Do not call external summarization tools for PDF parsing.
- If extraction is partial or truncated, say that clearly in the final answer.
- Keep any user caption or question as the primary intent signal.
