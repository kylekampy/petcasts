from pathlib import Path

from petcast.config import load_config


def test_styles_avoid_artist_name_prompting():
    config = load_config(Path(__file__).parents[1])
    blocked_terms = [
        "andy warhol",
        "braque",
        "du pasquier",
        "hanna-barbera",
        "hiroshige",
        "hokusai",
        "kirchner",
        "lichtenstein",
        "matisse",
        "nolde",
        "picasso",
        "roy lichtenstein",
        "sottsass",
    ]

    combined = "\n".join(config.styles).lower()

    assert "in the style of" not in combined
    assert not [term for term in blocked_terms if term in combined]
