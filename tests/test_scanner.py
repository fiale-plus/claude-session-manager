"""Tests for csm.scanner — path encoding and session discovery basics."""

from __future__ import annotations

from csm.scanner import encode_project_path


class TestEncodeProjectPath:
    def test_basic_path(self):
        assert encode_project_path("/Users/pavel/repos/foo") == "-Users-pavel-repos-foo"

    def test_nested_path(self):
        result = encode_project_path("/Users/pavel/repos/fiale-plus/claude-session-manager")
        assert result == "-Users-pavel-repos-fiale-plus-claude-session-manager"

    def test_trailing_slash_stripped(self):
        assert encode_project_path("/Users/pavel/repos/foo/") == "-Users-pavel-repos-foo"

    def test_root_path(self):
        assert encode_project_path("/") == "-"

    def test_home_path(self):
        assert encode_project_path("/Users/pavel") == "-Users-pavel"
