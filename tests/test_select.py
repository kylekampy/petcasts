from petcast.config import Pet
from petcast.select import Selection, load_history, record_selection


def test_record_selection_stores_weather_integration(tmp_path):
    selection = Selection(
        pets=[Pet("Alice", "gray cat", ["alice.png"])],
        photo="alice.png",
        style="poster",
    )

    record_selection(
        tmp_path,
        selection,
        scene_activity="Alice plays in the garden.",
        scene_weather_integration="Forecast painted on garden stones.",
    )

    history = load_history(tmp_path)

    assert history[0]["scene"] == {
        "activity": "Alice plays in the garden.",
        "weather_integration": "Forecast painted on garden stones.",
    }
