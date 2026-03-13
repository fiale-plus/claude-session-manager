"""CLI entry point for Claude Session Manager TUI."""

from __future__ import annotations


def main():
    from csm.app import SessionManagerApp

    SessionManagerApp().run()


if __name__ == "__main__":
    main()
