#!/usr/bin/env python3
"""Small deterministic regression suite for the CI report generators."""

from __future__ import annotations

import argparse
import csv
import json
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

import generate_aislop_report as aislop_report  # noqa: E402
import generate_qex_report as qex_report  # noqa: E402


def check(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)
    print(f"PASS: {message}")


def main() -> int:
    check(abs(qex_report.interpolate_p50([-4, 0, 4, 8], [0.0, 0.2, 0.6, 1.0]) - 3.0) < 1e-9,
          "P50 linearly interpolates the first 0.5 probability crossing")
    check(qex_report.isotonic_increasing([0.0, 0.8, 0.6, 1.0], [10, 10, 10, 10]) == [0.0, 0.7, 0.7, 1.0],
          "decode probabilities are isotonic-fitted without changing monotone series")
    check(qex_report.wilson_interval(5, 10)[0] < 0.5 < qex_report.wilson_interval(5, 10)[1],
          "Wilson 95 percent interval brackets a 5/10 measurement")

    with tempfile.TemporaryDirectory(prefix="satssdv-report-tests-") as temporary:
        temp = Path(temporary)
        race_log = temp / "race.log"
        race_log.write_text(
            "ok  \tgithub.com/BH2VSQ/SATSSDV-Go/internal/modem/mary\t11.970s\n"
            "ok  \tgithub.com/BH2VSQ/SATSSDV-Go/internal/rx\t(cached)\n",
            encoding="utf-8",
        )
        status, results = qex_report.read_race_log(race_log)
        check(status == "passed" and len(results) == 2,
              "race report parser recognizes package results with duration and cached markers")
        race_log.write_text("WARNING: DATA RACE\n", encoding="utf-8")
        status, _ = qex_report.read_race_log(race_log)
        check(status == "failed", "race report parser flags DATA RACE diagnostics")

        csv_path = temp / "one-profile.csv"
        fields = [
            "profile", "modulation", "bit_rate", "sample_rate", "scenario", "reference", "channel_model",
            "frequency_spread_hz", "differential_delay_ms", "scintillation_s4", "phase_sigma_rad",
            "scintillation_rate_hz", "fixed_loss_db", "shadow_exceedance_pct", "optical_shadowing_pct",
            "elevation_deg", "carrier_ghz", "measured_s4", "measured_phase_sigma_rad", "shadow_fade_db",
            "snr_2500_db", "trials", "successes", "probability", "per", "seed_base",
        ]
        with csv_path.open("w", newline="", encoding="utf-8") as output:
            writer = csv.DictWriter(output, fieldnames=fields)
            writer.writeheader()
            for snr, success in ((-4, 0), (0, 2), (4, 4)):
                row = {key: "0" for key in fields}
                row.update({
                    "profile": "ssdv-qpsk-1600", "modulation": "qpsk", "bit_rate": "3200",
                    "sample_rate": "48000", "scenario": "AWGN", "reference": "QEX baseline",
                    "channel_model": "AWGN", "snr_2500_db": str(snr), "trials": "4",
                    "successes": str(success), "probability": str(success / 4),
                    "per": str(1 - success / 4), "seed_base": "42",
                })
                writer.writerow(row)
        rows = qex_report.read_channel_csvs([csv_path])
        summaries = qex_report.aggregate_rows(rows)
        check(len(rows) == 3 and len(summaries) == 1 and summaries[0]["p50_db"] == 0,
              "CSV aggregation creates a valid P50 summary from raw CI measurements")
        combined = temp / "combined.csv"
        qex_report.write_combined_csv(rows, combined)
        check(combined.is_file() and len(list(csv.DictReader(combined.open(encoding="utf-8")))) == 3,
              "combined CSV preserves every raw measurement")

        sample_json = temp / "aislop.json"
        sample_json.write_text(json.dumps({
            "score": 91,
            "label": "Healthy",
            "cliVersion": "0.20.0",
            "coverage": {"supportedFiles": 12, "unsupportedFiles": 0},
            "engines": {"lint": {"issues": 0, "skipped": False, "elapsed": 10.0}},
            "summary": {"files": 12, "errors": 0, "warnings": 1, "fixable": 1},
            "diagnostics": [{"engine": "format", "rule": "python-formatting", "severity": "warning",
                             "filePath": "scripts/example.py", "message": "format warning", "line": 1,
                             "assessment": {"kind": "style-policy"}}],
        }), encoding="utf-8")
        result, error = aislop_report.read_result(sample_json)
        check(result is not None and not error, "Aislop JSON report parser accepts the v0.20 schema")
        args = argparse.Namespace(scan_outcome="success", scan_exit="0")
        report = aislop_report.build_report(result, "", args)
        check("python-formatting" in report and "91 (Healthy)" in report,
              "Aislop Markdown contains score and diagnostic-level findings")

        report_dir = temp / "qex-report"
        report_dir.mkdir()
        args = argparse.Namespace(trials=4, race_log=race_log)
        markdown = qex_report.report_markdown(summaries, args)
        check("Race / concurrency regression" in markdown and "P50" in markdown,
              "QEX Markdown includes P50 and race-regression results")

    print("PASS: all report generator tests")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
