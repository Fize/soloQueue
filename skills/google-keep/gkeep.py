#!/usr/bin/env python3
"""
Google Keep CLI script for soloQueue skill.
Supports environment variables (GOOGLE_KEEP_EMAIL, GOOGLE_KEEP_MASTER_TOKEN, GOOGLE_KEEP_OAUTH_TOKEN)
and auto-installs missing dependencies (gkeepapi, gpsoauth) on first run.
"""

import argparse
import json
import os
import subprocess
import sys
import urllib.parse
import urllib.request
import warnings

# Suppress urllib3 SSL warnings (e.g., LibreSSL compatibility)
warnings.filterwarnings("ignore")

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
VENV_DIR = os.path.join(SCRIPT_DIR, ".venv")


def ensure_dependencies():
    """Ensure gkeepapi and gpsoauth are installed in a local .venv."""
    # Check if venv exists and add to path
    if os.path.isdir(VENV_DIR):
        for lib_dir in ("lib", "Lib"):
            lib_path = os.path.join(VENV_DIR, lib_dir)
            if os.path.isdir(lib_path):
                for d in os.listdir(lib_path):
                    sp = os.path.join(lib_path, d, "site-packages")
                    if os.path.isdir(sp) and sp not in sys.path:
                        sys.path.insert(0, sp)
                        break

    try:
        import gkeepapi
        import gpsoauth

        return
    except ImportError:
        pass

    # Auto-create venv and install dependencies using local venv pip
    print("gkeepapi/gpsoauth missing. Creating local .venv and installing dependencies...", file=sys.stderr)
    try:
        if not os.path.isdir(VENV_DIR):
            import venv
            venv.create(VENV_DIR, with_pip=True)

        venv_python = os.path.join(VENV_DIR, "bin", "python")
        if not os.path.exists(venv_python):
            venv_python = os.path.join(VENV_DIR, "Scripts", "python.exe")

        subprocess.check_call(
            [venv_python, "-m", "pip", "install", "--quiet", "gkeepapi", "gpsoauth"]
        )

        for lib_dir in ("lib", "Lib"):
            lib_path = os.path.join(VENV_DIR, lib_dir)
            if os.path.isdir(lib_path):
                for d in os.listdir(lib_path):
                    sp = os.path.join(lib_path, d, "site-packages")
                    if os.path.isdir(sp) and sp not in sys.path:
                        sys.path.insert(0, sp)
                        break

        import gkeepapi
        import gpsoauth
    except Exception as e:
        print(f"Error auto-installing dependencies: {e}", file=sys.stderr)
        print("Please run: python3 -m venv .venv && .venv/bin/pip install gkeepapi gpsoauth", file=sys.stderr)
        sys.exit(1)


ensure_dependencies()
import gkeepapi
import gpsoauth

# --- Config & Credential Storage ---
CONFIG_DIR = os.environ.get("GKEEP_CONFIG_DIR", os.path.join(SCRIPT_DIR, ".config"))
TOKEN_FILE = os.path.join(CONFIG_DIR, "master_token")
STATE_FILE = os.path.join(CONFIG_DIR, "state.json")
EMAIL_FILE = os.path.join(CONFIG_DIR, "email")


def ensure_config_dir():
    os.makedirs(CONFIG_DIR, mode=0o700, exist_ok=True)


def get_credentials():
    """Extract email and master_token from env vars or saved config files."""
    email = os.environ.get("GOOGLE_KEEP_EMAIL") or os.environ.get("GKEEP_EMAIL")
    master_token = os.environ.get("GOOGLE_KEEP_MASTER_TOKEN") or os.environ.get(
        "GKEEP_MASTER_TOKEN"
    )
    oauth_token = os.environ.get("GOOGLE_KEEP_OAUTH_TOKEN") or os.environ.get(
        "GKEEP_OAUTH_TOKEN"
    )

    if not email and os.path.exists(EMAIL_FILE):
        with open(EMAIL_FILE) as f:
            email = f.read().strip()

    if not master_token and os.path.exists(TOKEN_FILE):
        with open(TOKEN_FILE) as f:
            master_token = f.read().strip()

    if email and oauth_token and not master_token:
        import urllib.parse

        # Clean and unquote oauth_token formatting (e.g. urlencoded %3A -> :)
        clean_oauth_token = urllib.parse.unquote(oauth_token.strip().strip('"').strip("'"))
        email = email.strip()
        # Exchange oauth_token for master_token
        android_id = os.environ.get("GOOGLE_KEEP_ANDROID_ID", "0123456789abcdef")
        try:
            res = gpsoauth.exchange_token(email, clean_oauth_token, android_id)
            if "Token" in res:
                master_token = res["Token"]
                ensure_config_dir()
                with open(TOKEN_FILE, "w") as f:
                    f.write(master_token)
                os.chmod(TOKEN_FILE, 0o600)
            else:
                # Retry with raw token if unquoted token failed
                res_raw = gpsoauth.exchange_token(email, oauth_token.strip().strip('"').strip("'"), android_id)
                if "Token" in res_raw:
                    master_token = res_raw["Token"]
                    ensure_config_dir()
                    with open(TOKEN_FILE, "w") as f:
                        f.write(master_token)
                    os.chmod(TOKEN_FILE, 0o600)
                else:
                    print(f"Failed to exchange oauth token: {res_raw}", file=sys.stderr)
        except Exception as e:
            print(f"Error exchanging OAuth token: {e}", file=sys.stderr)

    if not email or not master_token:
        print("Error: Missing Google Keep credentials.", file=sys.stderr)
        print(
            "Please set GOOGLE_KEEP_EMAIL and GOOGLE_KEEP_MASTER_TOKEN in environment variables.",
            file=sys.stderr,
        )
        sys.exit(1)

    # Save credentials locally if not saved
    ensure_config_dir()
    if not os.path.exists(EMAIL_FILE):
        with open(EMAIL_FILE, "w") as f:
            f.write(email)
        os.chmod(EMAIL_FILE, 0o600)

    if not os.path.exists(TOKEN_FILE):
        with open(TOKEN_FILE, "w") as f:
            f.write(master_token)
        os.chmod(TOKEN_FILE, 0o600)

    return email, master_token


def save_state(keep):
    ensure_config_dir()
    try:
        with open(STATE_FILE, "w") as f:
            json.dump(keep.dump(), f)
        os.chmod(STATE_FILE, 0o600)
    except Exception:
        pass


def load_state():
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE) as f:
                return json.load(f)
        except Exception:
            pass
    return None


def get_keep(sync=True):
    """Authenticate and optionally sync. Returns Keep instance."""
    email, token = get_credentials()
    keep = gkeepapi.Keep()
    state = load_state()

    if state:
        try:
            keep.resume(email, token, state=state)
        except Exception:
            keep.resume(email, token)
    else:
        keep.resume(email, token)

    if sync:
        keep.sync()
        save_state(keep)

    return keep


def note_to_dict(note):
    """Convert a note to a dict representation."""
    result = {
        "id": note.id,
        "title": note.title or "",
        "type": "list" if isinstance(note, gkeepapi.node.List) else "note",
        "color": note.color.name if note.color else "DEFAULT",
        "pinned": note.pinned,
        "archived": note.archived,
        "trashed": note.trashed,
        "labels": [l.name for l in note.labels.all()],
    }

    if isinstance(note, gkeepapi.node.List):
        items = []
        for item in note.items:
            items.append({
                "text": item.text,
                "checked": item.checked,
                "indented": item.indented,
            })
        result["items"] = items
        result["text"] = "\n".join(
            f"[{'x' if i.checked else ' '}] {i.text}" for i in note.items
        )
    else:
        result["text"] = note.text or ""

    return result


def find_note_by_id_or_title(keep, identifier):
    note = keep.get(identifier)
    if note:
        return note

    for n in keep.all():
        if n.title and n.title.lower() == identifier.lower():
            return n
    return None


def cmd_list(args):
    keep = get_keep(sync=True)
    notes = keep.all()

    if args.pinned:
        notes = [n for n in notes if n.pinned]
    if args.archived:
        notes = [n for n in notes if n.archived]

    if args.limit:
        notes = list(notes)[: args.limit]

    results = [note_to_dict(n) for n in notes]

    if args.json:
        print(json.dumps(results, ensure_ascii=False, indent=2))
    else:
        if not results:
            print("No notes found.")
            return
        for r in results:
            status = []
            if r["pinned"]:
                status.append("pinned")
            if r["archived"]:
                status.append("archived")
            status_str = f" ({', '.join(status)})" if status else ""
            print(
                f"- [{r['id']}] {r['type'].upper()}: {r['title'] or '(Untitled)'}{status_str}"
            )
            if r["text"]:
                preview = r["text"].split("\n")[0][:60]
                print(f"  {preview}")


def cmd_get(args):
    keep = get_keep(sync=True)
    note = find_note_by_id_or_title(keep, args.id_or_title)
    if not note:
        print(f"Error: Note '{args.id_or_title}' not found.", file=sys.stderr)
        sys.exit(1)

    note_dict = note_to_dict(note)
    if args.json:
        print(json.dumps(note_dict, ensure_ascii=False, indent=2))
    else:
        print(f"ID: {note_dict['id']}")
        print(f"Title: {note_dict['title'] or '(Untitled)'}")
        print(f"Type: {note_dict['type']}")
        print(f"Pinned: {note_dict['pinned']}")
        print(f"Archived: {note_dict['archived']}")
        print("---")
        print(note_dict["text"])


def cmd_create(args):
    keep = get_keep(sync=False)

    if args.is_list or args.items:
        items = args.items or []
        if args.text and not items:
            items = [line.strip() for line in args.text.split("\n") if line.strip()]
        note = keep.createList(args.title or "", [(item, False) for item in items])
    else:
        note = keep.createNote(args.title or "", args.text or "")

    if args.pinned:
        note.pinned = True

    keep.sync()
    save_state(keep)

    note_dict = note_to_dict(note)
    if args.json:
        print(json.dumps(note_dict, ensure_ascii=False, indent=2))
    else:
        print(
            f"Created {note_dict['type']} '{note_dict['title']}' (ID: {note_dict['id']})"
        )


def cmd_search(args):
    keep = get_keep(sync=True)
    query = args.query.lower()
    matches = []

    for note in keep.all():
        title = note.title or ""
        text = note.text or ""
        if query in title.lower() or query in text.lower():
            matches.append(note_to_dict(note))

    if args.json:
        print(json.dumps(matches, ensure_ascii=False, indent=2))
    else:
        if not matches:
            print(f"No notes matching '{args.query}'.")
            return
        for r in matches:
            print(f"- [{r['id']}] {r['title'] or '(Untitled)'}")


def cmd_delete(args):
    keep = get_keep(sync=False)
    note = find_note_by_id_or_title(keep, args.id_or_title)
    if not note:
        print(f"Error: Note '{args.id_or_title}' not found.", file=sys.stderr)
        sys.exit(1)

    note.delete()
    keep.sync()
    save_state(keep)
    print(f"Deleted note '{args.id_or_title}'")


def cmd_add_item(args):
    keep = get_keep(sync=False)
    note = find_note_by_id_or_title(keep, args.id_or_title)
    if not note:
        print(f"Error: Note '{args.id_or_title}' not found.", file=sys.stderr)
        sys.exit(1)

    if not isinstance(note, gkeepapi.node.List):
        print(
            f"Error: Note '{args.id_or_title}' is not a checklist list.", file=sys.stderr
        )
        sys.exit(1)

    note.add(args.text, False)
    keep.sync()
    save_state(keep)
    print(f"Added item '{args.text}' to list '{note.title or note.id}'")


def main():
    parser = argparse.ArgumentParser(description="Google Keep CLI wrapper")
    subparsers = parser.add_subparsers(dest="subcommand", help="Sub-command to execute")

    # list
    p_list = subparsers.add_parser("list", help="List notes")
    p_list.add_argument("--pinned", action="store_true", help="List pinned notes only")
    p_list.add_argument("--archived", action="store_true", help="List archived notes")
    p_list.add_argument("--limit", type=int, help="Limit number of notes")
    p_list.add_argument("--json", action="store_true", help="Output as JSON")
    p_list.set_defaults(func=cmd_list)

    # get
    p_get = subparsers.add_parser("get", help="Get note by ID or title")
    p_get.add_argument("id_or_title", help="Note ID or title")
    p_get.add_argument("--json", action="store_true", help="Output as JSON")
    p_get.set_defaults(func=cmd_get)

    # create
    p_create = subparsers.add_parser("create", help="Create a new note or list")
    p_create.add_argument("--title", help="Note title")
    p_create.add_argument("--text", help="Note body text")
    p_create.add_argument(
        "--items", nargs="*", help="List items if creating a checklist"
    )
    p_create.add_argument(
        "--list", dest="is_list", action="store_true", help="Create as checklist"
    )
    p_create.add_argument("--pinned", action="store_true", help="Pin the note")
    p_create.add_argument("--json", action="store_true", help="Output as JSON")
    p_create.set_defaults(func=cmd_create)

    # search
    p_search = subparsers.add_parser("search", help="Search notes")
    p_search.add_argument("query", help="Search term")
    p_search.add_argument("--json", action="store_true", help="Output as JSON")
    p_search.set_defaults(func=cmd_search)

    # delete
    p_delete = subparsers.add_parser("delete", help="Delete a note")
    p_delete.add_argument("id_or_title", help="Note ID or title")
    p_delete.set_defaults(func=cmd_delete)

    # add-item
    p_item = subparsers.add_parser("add-item", help="Add item to a list note")
    p_item.add_argument("id_or_title", help="List note ID or title")
    p_item.add_argument("text", help="Item text")
    p_item.set_defaults(func=cmd_add_item)

    args = parser.parse_args()
    if not args.subcommand:
        parser.print_help()
        sys.exit(0)

    args.func(args)


if __name__ == "__main__":
    main()
