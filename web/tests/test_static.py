from pathlib import Path


def test_live_javascript_has_resumable_sse_and_visibility_fallback():
    source = Path("autoresearch_ui/static/live.js").read_text()
    assert "new EventSource" in source
    assert "lastEventId" in source
    assert "contentvisibilityautostatechange" in source
    assert "IntersectionObserver" in source
    assert "addEventListener" in source


def test_theme_has_accessible_targets_and_responsive_tables():
    source = Path("autoresearch_ui/static/theme.css").read_text()
    assert "content-visibility: auto" in source
    assert "contain-intrinsic-size" in source
    assert "@container" in source
    assert "position: sticky" in source
    assert "pointer: coarse" in source
    assert ":focus-visible" in source
