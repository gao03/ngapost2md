#!/usr/bin/env python3
import argparse
import json
import sys
import webbrowser
from datetime import datetime
from html import escape
from pathlib import Path


def text(value):
    return escape(str(value or ""), quote=True)


def thread_url(data):
    if data.get("threadUrl"):
        return data.get("threadUrl")
    if data.get("tid"):
        return f"https://bbs.nga.cn/read.php?tid={data.get('tid')}"
    return "#"


def analyzed_at(data):
    return data.get("analyzedAt") or datetime.now().strftime("%Y-%m-%d %H:%M")


def card(item):
    return (
        '<article class="card">'
        f"<h3>{text(item.get('title'))}</h3>"
        f"<p>{text(item.get('body'))}</p>"
        "</article>"
    )


def opinion(item):
    role = item.get("role") or item.get("title")
    title = item.get("title") or role
    body = item.get("body")
    return (
        '<article class="card">'
        f'<span class="tag note">{text(role)}</span>'
        f"<h3>{text(title)}</h3>"
        f"<p>{text(body)}</p>"
        "</article>"
    )


def dispute(item):
    sides = item.get("sides") or []
    side_html = []
    for side in sides[:2]:
        side_html.append(
            '<div class="side">'
            f"<b>{text(side.get('label'))}</b>"
            f"{text(side.get('body'))}"
            "</div>"
        )
    return (
        '<article class="dispute">'
        f"<h3>{text(item.get('title'))}</h3>"
        f'<div class="sides">{"".join(side_html)}</div>'
        "</article>"
    )


def heat_item(item):
    level = int(item.get("level") or 1)
    level = max(1, min(level, 4))
    level_label = {4: "多数", 3: "不少", 2: "少数", 1: "个别"}[level]
    bars = "<span></span>" * 4
    return (
        f'<article class="heat-item hot-{level}">'
        "<div>"
        f'<div class="heat-name">{text(item.get("label"))}</div>'
        f'<span class="heat-level">{level_label}</span>'
        "</div>"
        "<div>"
        f'<div class="heat-meter" aria-label="讨论热度 {level}/4">{bars}</div>'
        f'<div class="heat-desc">{text(item.get("note"))}</div>'
        "</div>"
        "</article>"
    )


def evidence(item):
    return (
        '<article class="evidence">'
        f"<h3>{text(item.get('title'))}</h3>"
        f"<p>{text(item.get('body'))}</p>"
        "</article>"
    )


def join_items(data, key, renderer):
    items = data.get(key) or []
    if not isinstance(items, list):
        raise ValueError(f"{key} must be a list")
    return "\n".join(renderer(item) for item in items)


def validate(data):
    required_strings = ["title", "tid", "floorRange", "tldr", "sourcePath"]
    missing = [key for key in required_strings if not data.get(key)]
    if missing:
        raise ValueError("missing required fields: " + ", ".join(missing))

    required_lists = ["summaryCards", "opinionMap", "disputes", "heatItems", "evidence"]
    missing_lists = [key for key in required_lists if not isinstance(data.get(key), list) or not data.get(key)]
    if missing_lists:
        raise ValueError("missing or empty list fields: " + ", ".join(missing_lists))


def render(data, template):
    validate(data)
    replacements = {
        "{{TITLE}}": text(data.get("title")),
        "{{TID}}": text(data.get("tid")),
        "{{THREAD_URL}}": text(thread_url(data)),
        "{{FLOOR_RANGE}}": text(data.get("floorRange")),
        "{{ANALYZED_AT}}": text(analyzed_at(data)),
        "{{TLDR}}": text(data.get("tldr")),
        "{{SUMMARY_CARDS}}": join_items(data, "summaryCards", card),
        "{{OPINION_MAP}}": join_items(data, "opinionMap", opinion),
        "{{DISPUTES}}": join_items(data, "disputes", dispute),
        "{{HEAT_ITEMS}}": join_items(data, "heatItems", heat_item),
        "{{EVIDENCE}}": join_items(data, "evidence", evidence),
        "{{AGENT_TAKE}}": text(data.get("agentTake")),
        "{{SOURCE_PATH}}": text(data.get("sourcePath")),
    }
    html = template
    for placeholder, value in replacements.items():
        html = html.replace(placeholder, value)
    return html


def main():
    parser = argparse.ArgumentParser(description="Render forum opinion report HTML.")
    parser.add_argument("data", help="Path to report-data.json")
    parser.add_argument("-o", "--output", required=True, help="Output HTML path")
    parser.add_argument("--template", help="Template path")
    parser.add_argument("--open", action="store_true", help="Open the rendered HTML in the default browser")
    args = parser.parse_args()

    script_dir = Path(__file__).resolve().parent
    skill_dir = script_dir.parent
    template_path = Path(args.template) if args.template else skill_dir / "references" / "forum-opinion-report-template.html"

    try:
        data = json.loads(Path(args.data).read_text(encoding="utf-8"))
        template = template_path.read_text(encoding="utf-8")
        output = render(data, template)
    except Exception as exc:
        print(f"render_forum_report.py: {exc}", file=sys.stderr)
        raise SystemExit(1)

    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(output, encoding="utf-8")
    if args.open:
        webbrowser.open(output_path.resolve().as_uri())
    print(output_path)


if __name__ == "__main__":
    main()
