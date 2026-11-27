#!/usr/bin/env python3
"""Send a formatted FLYX release announcement to a Telegram channel."""

import argparse
import html
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request

MAX_MESSAGE_LEN = 4096


def extract_changelog_section(changelog_path: str, version: str) -> str:
    if not os.path.isfile(changelog_path):
        return ""
    lines = []
    found = False
    started = False
    date_re = re.compile(r"^\d{4}-\d{2}-\d{2}$")
    with open(changelog_path, "r", encoding="utf-8") as f:
        for raw in f:
            line = raw.rstrip("\n")
            if line.startswith("## "):
                if found:
                    break
                parts = line.split()
                if len(parts) >= 2 and parts[1] == version:
                    found = True
                continue
            if not found:
                continue
            if not started and date_re.match(line.strip()):
                continue
            if line.strip():
                started = True
            if started:
                lines.append(line)
    while lines and not lines[-1].strip():
        lines.pop()
    return "\n".join(lines)


def fallback_git_log(version: str, sha: str) -> str:
    try:
        prev = subprocess.check_output(
            ["git", "describe", "--tags", "--abbrev=0", "--exclude", version, sha],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
    except subprocess.CalledProcessError:
        prev = ""

    rev_range = f"{prev}..{sha}" if prev else sha
    try:
        out = subprocess.check_output(
            ["git", "log", "--no-merges", "-n", "20", "--format=- %s", rev_range],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
        return out or "- 例行维护与问题修复"
    except subprocess.CalledProcessError:
        return "- 例行维护与问题修复"


def resolve_doc_links(text: str, docs_base: str) -> str:
    text = re.sub(
        r"\]\(\./guide/([^)#]+)\.md",
        rf"]({docs_base}/guide/\1",
        text,
    )
    text = re.sub(
        r"\]\(\./([^)#]+)\.md",
        rf"]({docs_base}/\1",
        text,
    )
    return text


def md_inline_to_html(text: str) -> str:
    """Convert inline Markdown (`code`, [text](url), **bold**) to Telegram HTML."""
    placeholders = []

    def stash(html_snippet: str) -> str:
        idx = len(placeholders)
        placeholders.append(html_snippet)
        return f"\x00PH{idx}\x00"

    # 1. Inline code first so contents are never parsed as links/bold.
    def repl_code(m: re.Match) -> str:
        return stash(f"<code>{html.escape(m.group(1))}</code>")

    text = re.sub(r"`([^`]+)`", repl_code, text)

    # 2. Markdown links [label](url).
    def repl_link(m: re.Match) -> str:
        label = html.escape(m.group(1))
        url = html.escape(m.group(2), quote=True)
        return stash(f'<a href="{url}">{label}</a>')

    text = re.sub(r"\[([^\]]+)\]\((https?://[^)\s]+)\)", repl_link, text)

    # 3. Escape remaining HTML characters.
    text = html.escape(text)

    # 4. Bold **...** (may contain placeholders like <code>...</code>).
    text = re.sub(r"\*\*(.+?)\*\*", r"<b>\1</b>", text)

    # 5. Restore stashed HTML snippets.
    for idx, snippet in enumerate(placeholders):
        text = text.replace(f"\x00PH{idx}\x00", snippet)

    return text


def format_body_lines(changes: str, docs_base: str) -> list[str]:
    changes = resolve_doc_links(changes, docs_base)
    formatted = []
    for raw in changes.splitlines():
        if not raw.strip():
            formatted.append("")
            continue
        m_nested = re.match(r"^(\s{2,})[-*]\s+(.*)$", raw)
        m_top = re.match(r"^[-*]\s+(.*)$", raw)
        if m_nested:
            formatted.append(f"  ◦ {md_inline_to_html(m_nested.group(2))}")
        elif m_top:
            formatted.append(f"• {md_inline_to_html(m_top.group(1))}")
        else:
            formatted.append(md_inline_to_html(raw))
    return formatted


def build_message(version: str, repo: str, changelog_path: str, sha: str) -> str:
    owner, name = repo.split("/", 1)
    docs_base = f"https://{owner}.github.io/{name}"
    release_url = f"https://github.com/{repo}/releases/tag/{version}"

    changes = extract_changelog_section(changelog_path, version)
    if not changes.strip():
        changes = fallback_git_log(version, sha)

    body_lines = format_body_lines(changes, docs_base)

    header = f"🚀 <b>FLYX v{html.escape(version)} 发布</b>\n\n"
    footer = (
        "\n\n"
        f'🔗 <a href="{html.escape(release_url, quote=True)}">GitHub Release</a> · '
        f'<a href="{html.escape(docs_base + "/changelog", quote=True)}">更新日志</a> · '
        f'<a href="{html.escape(docs_base + "/demo/", quote=True)}">在线演示</a> · '
        '<a href="https://t.me/flyx_chat">交流群</a>'
    )
    trunc_note = "\n… 更多改动请查看完整更新日志"

    budget = MAX_MESSAGE_LEN - len(header) - len(footer)
    body = "\n".join(body_lines)
    if len(body) > budget:
        kept = []
        used = 0
        limit = budget - len(trunc_note)
        for line in body_lines:
            add = len(line) + (1 if kept else 0)
            if used + add > limit:
                break
            kept.append(line)
            used += add
        body = "\n".join(kept) + trunc_note

    return header + body + footer


def send_telegram(token: str, chat_id: str, text: str) -> None:
    url = f"https://api.telegram.org/bot{token}/sendMessage"
    payload = json.dumps(
        {
            "chat_id": chat_id,
            "text": text,
            "parse_mode": "HTML",
            "disable_web_page_preview": True,
        }
    ).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            if not data.get("ok"):
                raise RuntimeError(f"Telegram API error: {data}")
            msg_id = data.get("result", {}).get("message_id")
            print(f"✅ 已推送到 Telegram 频道 {chat_id} (message_id={msg_id})")
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Telegram HTTP {e.code}: {err_body}") from e


def main() -> int:
    parser = argparse.ArgumentParser(description="Notify Telegram channel on release.")
    parser.add_argument("version", help="Release version, e.g. 3.10.3")
    parser.add_argument("repo", help="GitHub repository, e.g. kure29/flyx")
    parser.add_argument("--sha", default="HEAD", help="Commit SHA being released")
    parser.add_argument("--changelog", default="doc/changelog.md", help="Path to changelog")
    parser.add_argument(
        "--chat-id",
        default=os.environ.get("TELEGRAM_CHAT_ID", "@flyx_notify"),
        help="Telegram channel username or chat ID",
    )
    parser.add_argument("--dry-run", action="store_true", help="Print message without sending")
    args = parser.parse_args()

    msg = build_message(args.version, args.repo, args.changelog, args.sha)
    if args.dry_run:
        print(msg)
        print(f"\n--- length: {len(msg)} / {MAX_MESSAGE_LEN} ---")
        return 0

    token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    if not token:
        print("::notice::TELEGRAM_BOT_TOKEN 未配置，跳过 Telegram 频道推送")
        return 0

    chat_id = (args.chat_id or "@flyx_notify").strip()
    send_telegram(token, chat_id, msg)
    return 0


if __name__ == "__main__":
    sys.exit(main())
