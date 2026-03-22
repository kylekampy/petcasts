"""CLI entry point for petcast."""

import argparse
import sys
from pathlib import Path

from dotenv import load_dotenv


def main() -> None:
    load_dotenv()
    parser = argparse.ArgumentParser(description="Petcast: pet weather forecast for e-ink")
    sub = parser.add_subparsers(dest="command")

    # generate
    gen = sub.add_parser("generate", help="Run the full generation pipeline")
    gen.add_argument("--debug", action="store_true", help="Save intermediate images to debug dir")
    gen.add_argument("--battery", type=float, default=None, help="Battery percentage (0-100)")
    gen.add_argument("--style", type=str, default=None, help="Force a specific style (substring match)")
    gen.add_argument("--root", type=Path, default=Path("."), help="Project root directory")

    # weather (standalone test)
    wx = sub.add_parser("weather", help="Test weather fetch")
    wx.add_argument("--root", type=Path, default=Path("."), help="Project root directory")

    # select (standalone test)
    sel = sub.add_parser("select", help="Test pet/style selection")
    sel.add_argument("--root", type=Path, default=Path("."), help="Project root directory")
    sel.add_argument("--count", type=int, default=5, help="Number of selections to test")

    # serve
    srv = sub.add_parser("serve", help="Start HTTP server for frame-driven generation")
    srv.add_argument("--root", type=Path, default=Path("."), help="Project root directory")
    srv.add_argument("--port", type=int, default=7777, help="Port to listen on")

    args = parser.parse_args()

    if args.command == "generate":
        from petcast.pipeline import run
        run(args.root.resolve(), debug=args.debug, battery_pct=args.battery, force_style=args.style)

    elif args.command == "weather":
        from petcast.config import load_config
        from petcast.weather import fetch_forecast
        config = load_config(args.root.resolve())
        forecast = fetch_forecast(config)
        print(f"Location: {config.location.name}")
        print(f"Weather:  {forecast['weather_icon']} {forecast['weather_desc']}")
        print(f"High/Low: {forecast['high_f']:.0f}°F / {forecast['low_f']:.0f}°F")
        print(f"Precip:   {forecast['precip_chance']}%")
        print(f"Wind:     {forecast['wind_mph']:.0f} mph")
        print(f"Sunrise:  {forecast['sunrise']}")
        print(f"Sunset:   {forecast['sunset']}")

    elif args.command == "select":
        from petcast.config import load_config
        from petcast.select import select
        config = load_config(args.root.resolve())
        for i in range(args.count):
            sel_result = select(config, args.root.resolve())
            pets = ", ".join(p.name for p in sel_result.pets)
            print(f"  #{i + 1}: {pets} | {sel_result.photo} | {sel_result.style}")

    elif args.command == "serve":
        from petcast.server import serve
        serve(args.root.resolve(), port=args.port)

    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
