from pathlib import Path


def test_live_javascript_has_resumable_sse_and_visibility_fallback():
    source = Path("autoresearch_ui/static/live.js").read_text()
    assert "new EventSource" in source
    assert "lastEventId" in source
    assert "contentvisibilityautostatechange" in source
    assert "IntersectionObserver" in source
    assert "addEventListener" in source


def test_graph_javascript_consumes_normalized_elements_without_snapshot_conversion():
    source = Path("autoresearch_ui/static/graph.js").read_text()
    assert "fetch('/internal/graph/elements'" in source
    assert "graphElementsFromSnapshot" not in source
    assert "from_node" not in source
    assert "to_node" not in source


def test_theme_has_accessible_targets_and_responsive_tables():
    source = Path("autoresearch_ui/static/theme.css").read_text()
    assert "content-visibility: auto" in source
    assert "contain-intrinsic-size" in source
    assert "@container" in source
    assert "position: sticky" in source
    assert "pointer: coarse" in source
    assert ":focus-visible" in source


def test_login_javascript_has_accessible_toggle_without_secret_persistence_or_logging():
    source = Path("autoresearch_ui/static/login.js").read_text()
    assert "addEventListener('click'" in source
    assert "aria-pressed" in source
    assert "input.type = showPassword ? 'text' : 'password'" in source
    assert "localStorage" not in source
    assert "sessionStorage" not in source
    assert "console." not in source
    assert "fetch(" not in source


def test_browser_accepts_owner_only_token_file():
    source = Path("tests/browser_smoke.js").read_text()
    assert "AUTORESEARCH_OPERATOR_TOKEN_FILE" in source
    assert "readFileSync" in source
    assert "lstatSync" in source
    assert "stat.uid" in source
    assert "stat.mode" in source
