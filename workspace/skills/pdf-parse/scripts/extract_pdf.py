#!/usr/bin/env python3

import argparse
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Extract text locally from a PDF file.")
    parser.add_argument("pdf_path", help="Path to a local PDF file")
    parser.add_argument("--max-pages", type=int, default=100, help="Maximum number of pages to extract")
    parser.add_argument("--max-chars", type=int, default=120000, help="Maximum number of output characters")
    return parser.parse_args()


def resolve_pdf(path_arg: str) -> Path:
    path = Path(path_arg).expanduser().resolve()
    if not path.exists():
        raise RuntimeError(f"pdf file not found: {path}")
    if not path.is_file():
        raise RuntimeError(f"pdf path is not a file: {path}")
    if path.suffix.lower() != ".pdf":
        raise RuntimeError(f"unsupported file type {path.suffix or '<none>'}; expected .pdf")
    return path


def extract_pdf_text(pdf_path: Path, max_pages: int, max_chars: int) -> str:
    try:
        from pypdf import PdfReader
    except ImportError as exc:
        raise RuntimeError("missing dependency: install pypdf to parse PDF files locally") from exc

    reader = PdfReader(str(pdf_path))
    if reader.is_encrypted:
        raise RuntimeError("encrypted PDFs are not supported")

    sections: list[str] = []
    extracted_any_text = False
    total_chars = 0
    truncated = False

    for index, page in enumerate(reader.pages, start=1):
        if index > max_pages:
            truncated = True
            break

        text = (page.extract_text() or "").strip()
        if text:
            extracted_any_text = True
        else:
            text = "[No extractable text on this page]"

        section = f"[Page {index}]\n{text}"
        if total_chars + len(section) > max_chars:
            remaining = max_chars - total_chars
            if remaining > 0:
                section = section[:remaining].rstrip()
                sections.append(section)
            truncated = True
            break

        sections.append(section)
        total_chars += len(section) + 2

    if not extracted_any_text:
        raise RuntimeError("no extractable text found; the PDF may be scanned or image-only")

    output = "\n\n".join(sections).strip()
    if truncated:
        output += "\n\n[TRUNCATED: output limit reached]"
    return output


def main() -> int:
    args = parse_args()

    try:
        pdf_path = resolve_pdf(args.pdf_path)
        text = extract_pdf_text(pdf_path, max_pages=max(args.max_pages, 1), max_chars=max(args.max_chars, 1))
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
