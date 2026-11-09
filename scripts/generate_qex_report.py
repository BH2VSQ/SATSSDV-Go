#!/usr/bin/env python3
"""Aggregate modem channel CSV data into QEX/ITU Markdown, JSON and plots."""

from __future__ import annotations

import argparse
import csv
import json
import os
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any



def read_channel_csvs(paths: list[Path]) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    for path in paths:
        if not path.is_file():
            continue
        with path.open(newline="", encoding="utf-8") as source:
            rows.extend(dict(row) for row in csv.DictReader(source))
    return rows


def wilson_interval(successes: int, trials: int, z: float = 1.96) -> tuple[float, float]:
    if trials <= 0:
        return 0.0, 1.0
    probability = successes / trials
    denominator = 1 + z * z / trials
    centre = (probability + z * z / (2 * trials)) / denominator
    margin = z * ((probability * (1 - probability) / trials + z * z / (4 * trials * trials)) ** 0.5) / denominator
    return max(0.0, centre - margin), min(1.0, centre + margin)


def isotonic_increasing(values: list[float], weights: list[int]) -> list[float]:
    blocks: list[list[float]] = []
    for index, (value, weight) in enumerate(zip(values, weights)):
        blocks.append([float(index), float(index), value * weight, float(weight)])
        while len(blocks) > 1:
            left, right = blocks[-2], blocks[-1]
            if left[2] / left[3] <= right[2] / right[3]:
                break
            blocks[-2:] = [[left[0], right[1], left[2] + right[2], left[3] + right[3]]]
    fitted = [0.0] * len(values)
    for start, end, total, weight in blocks:
        for index in range(int(start), int(end) + 1):
            fitted[index] = total / weight
    return fitted


def interpolate_p50(snr_values: list[float], probabilities: list[float]) -> float | None:
    for index, probability in enumerate(probabilities):
        if probability >= 0.5:
            if index == 0:
                return snr_values[0]
            lower_p, upper_p = probabilities[index - 1], probability
            lower_snr, upper_snr = snr_values[index - 1], snr_values[index]
            if upper_p == lower_p:
                return upper_snr
            fraction = (0.5 - lower_p) / (upper_p - lower_p)
            return lower_snr + fraction * (upper_snr - lower_snr)
    return None


def summarize_group(group_rows: list[dict[str, str]]) -> dict[str, Any]:
    ordered = sorted(group_rows, key=lambda row: float(row["snr_2500_db"]))
    snr_values = [float(row["snr_2500_db"]) for row in ordered]
    trials = [int(row["trials"]) for row in ordered]
    successes = [int(row["successes"]) for row in ordered]
    raw_probabilities = [success / trial if trial else 0.0 for success, trial in zip(successes, trials)]
    probabilities = isotonic_increasing(raw_probabilities, trials)
    intervals = [wilson_interval(success, trial) for success, trial in zip(successes, trials)]
    p50 = interpolate_p50(snr_values, probabilities)
    lower_probabilities = [interval[0] for interval in intervals]
    upper_probabilities = [interval[1] for interval in intervals]
    p50_low = interpolate_p50(snr_values, upper_probabilities)
    p50_high = interpolate_p50(snr_values, lower_probabilities)
    first = ordered[0]
    return {
        "profile": first["profile"],
        "modulation": first["modulation"],
        "bit_rate": int(first["bit_rate"]),
        "scenario": first["scenario"],
        "reference": first["reference"],
        "channel_model": first["channel_model"],
        "frequency_spread_hz": float(first["frequency_spread_hz"]),
        "differential_delay_ms": float(first["differential_delay_ms"]),
        "scintillation_s4": float(first["scintillation_s4"]),
        "measured_s4": float(first.get("measured_s4", 0) or 0),
        "phase_sigma_rad": float(first.get("phase_sigma_rad", 0) or 0),
        "measured_phase_sigma_rad": float(first.get("measured_phase_sigma_rad", 0) or 0),
        "shadow_fade_db": float(first.get("shadow_fade_db", 0) or 0),
        "fixed_loss_db": float(first.get("fixed_loss_db", 0) or 0),
        "shadow_exceedance_pct": float(first.get("shadow_exceedance_pct", 0) or 0),
        "optical_shadowing_pct": float(first.get("optical_shadowing_pct", 0) or 0),
        "elevation_deg": float(first.get("elevation_deg", 0) or 0),
        "carrier_ghz": float(first.get("carrier_ghz", 0) or 0),
        "snr_db": snr_values,
        "trials": trials,
        "successes": successes,
        "probability": probabilities,
        "raw_probability": raw_probabilities,
        "wilson_95": [[low, high] for low, high in intervals],
        "p50_db": p50,
        "p50_95_low_db": p50_low,
        "p50_95_high_db": p50_high,
    }


def aggregate_rows(rows: list[dict[str, str]]) -> list[dict[str, Any]]:
    groups: dict[tuple[str, str], list[dict[str, str]]] = defaultdict(list)
    for row in rows:
        groups[(row["profile"], row["scenario"])].append(row)
    return [summarize_group(group) for _, group in sorted(groups.items())]


def write_combined_csv(rows: list[dict[str, str]], output: Path) -> None:
    if not rows:
        output.write_text("", encoding="utf-8")
        return
    fieldnames = list(rows[0])
    with output.open("w", newline="", encoding="utf-8") as destination:
        writer = csv.DictWriter(destination, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def plot_probability_curves(summaries: list[dict[str, Any]], output: Path) -> list[Path]:
    import matplotlib

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt

    images: list[Path] = []
    for family, predicate in (
        ("awgn", lambda item: item["scenario"] == "AWGN"),
        ("qex", lambda item: item["scenario"].startswith("QEX_")),
        ("p531", lambda item: item["scenario"].startswith("ITU_P531_")),
        ("itu_environment", lambda item: item["scenario"].startswith(("ITU_P681_", "ITU_P618_"))),
    ):
        selected = [item for item in summaries if predicate(item)]
        if not selected:
            continue
        figure, axis = plt.subplots(figsize=(10, 6))
        for item in selected:
            label = f"{item['profile']} / {item['scenario']}"
            axis.plot(item["snr_db"], item["probability"], marker="o", label=label)
            axis.fill_between(
                item["snr_db"],
                [interval[0] for interval in item["wilson_95"]],
                [interval[1] for interval in item["wilson_95"]],
                alpha=0.12,
            )
        axis.axhline(0.5, linestyle="--", linewidth=1)
        axis.set(xlabel="SNR in 2500 Hz reference bandwidth (dB)", ylabel="Valid packet decode probability", ylim=(-0.03, 1.03), title=f"SATSSDV-Go {family.upper()} channel sensitivity")
        axis.grid(True, alpha=0.25)
        axis.legend(fontsize=7, loc="best")
        figure.tight_layout()
        path = output / f"channel-{family}-probability.png"
        figure.savefig(path, dpi=160)
        plt.close(figure)
        images.append(path)
    return images


def read_race_log(path: Path | None) -> tuple[str, list[str]]:
    if path is None or not path.is_file():
        return "missing", []
    content = path.read_text(encoding="utf-8", errors="replace")
    import re

    result_lines = [
        line.strip() for line in content.splitlines()
        if re.search(r"\bok\s+[^\s]+(?:\s+\([^)]*\))?(?:\s+\d+(?:\.\d+)?s)?$|\bFAIL\s+|WARNING: DATA RACE", line)
    ]
    failed = any("WARNING: DATA RACE" in line or line.lstrip().startswith("FAIL") for line in result_lines)
    packages = sum(1 for line in result_lines if re.search(r"\bok\s+[^\s]+", line))
    status = "failed" if failed else "passed" if packages else "no package results"
    return status, result_lines


def report_markdown(summaries: list[dict[str, Any]], args: argparse.Namespace) -> str:
    timestamp = datetime.now(timezone.utc).isoformat(timespec="seconds")
    race_status, race_results = read_race_log(args.race_log)
    lines = [
        "# QEX / ITU Satellite-Channel Sensitivity Report",
        "",
        f"- Generated: `{timestamp}`",
        f"- Commit: `{os.getenv('GITHUB_SHA', 'local/unknown')}`",
        f"- Workflow run: `{os.getenv('GITHUB_RUN_ID', 'local')}`",
        f"- Trials per SNR point (configured): `{args.trials}`",
        f"- Channel groups measured: `{len(summaries)}`",
        f"- Race detector status: `{race_status}`",
        "- SNR reference: `2500 Hz equivalent`",
        "",
        "## Scope and interpretation",
        "",
        "The modem is tested through the production `StreamPackets` transmitter and `StreamDecoder` receiver. Each row is a CRC/packet-validated decode. The SNR axis is the integrated signal-to-noise ratio normalized to a 2 500 Hz reference bandwidth; it is not receiver RF input power, dBm sensitivity, or Eb/N0.",
        "",
        "The QEX method uses decode probability versus SNR, with a 50% crossing (P50) reported per mode and channel. In the cited QEX FT4/FT8 study, two independently fading paths have equal mean attenuation and equal frequency spread, with differential path delay as the second channel parameter. The same methodology is adapted here to the project's production transmitter/receiver; QEX's numerical HF thresholds are not copied as SATSSDV pass/fail limits.",
        "",
        "QEX HF channel parameters are used as reproducible stress profiles only. They are not asserted to be VHF/UHF satellite propagation predictions. The ITU cases are kept separate: P.531 uses the Recommendation's S4-to-Nakagami-m intensity marginal with a simplified temporally correlated fade and Gaussian phase process; this is not the complete GISM or full P.531 channel simulator. The P.681 case uses the LMSS roadside-tree-shadow quantile equation only inside its stated model envelope and adds a separately labelled synthetic echo. The P.618 row is a fixed 3 dB stress-margin test, not a P.618 site-specific attenuation prediction.",
        "",
        "## QEX paper reference thresholds (not project pass/fail limits)",
        "",
        "The following are the QEX July/August 2020 Table 6 reference values for FT4/FT8, shown to preserve the source paper's context. They are not SATSSDV-Go thresholds and should not be used to rank these unrelated modem implementations directly.",
        "",
        "| QEX channel | Spread (Hz) | Differential delay (ms) | FT4 no AP (dB) | FT4 max AP (dB) | FT8 no AP (dB) | FT8 max AP (dB) |",
        "|---|---:|---:|---:|---:|---:|---:|",
        "| AWGN | 0.0 | 0.0 | -17.5 | -19.5 | -20.8 | -22.7 |",
        "| Mid-latitude quiet | 0.1 | 0.5 | -17.4 | -19.4 | -20.0 | -22.4 |",
        "| Mid-latitude moderate | 0.5 | 1.0 | -15.8 | -18.6 | -18.8 | -22.1 |",
        "| Mid-latitude disturbed | 1.0 | 2.0 | -15.2 | -18.4 | -18.6 | -22.1 |",
        "| High-latitude moderate | 10.0 | 3.0 | -12.2 | -17.4 | -8.6 | -18.9 |",
        "",
        "## P50 decoding threshold by mode and scenario",
        "",
        "P50 is interpolated from the isotonic non-decreasing fit of measured decode probabilities. Confidence bounds use Wilson 95% intervals at each SNR point; the reported P50 interval is a conservative crossing envelope, not a bootstrap confidence interval for the complete curve.",
        "",
        "| Profile | Scenario | Trials/point | P50 SNR (dB) | P50 interval (dB) | Spread (Hz) | Delay (ms) | S4 target/measured | Loss (dB) |",
        "|---|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for item in summaries:
        p50 = f"{item['p50_db']:.2f}" if item["p50_db"] is not None else "> tested range"
        if item["p50_95_low_db"] is not None and item["p50_95_high_db"] is not None:
            interval = f"{item['p50_95_low_db']:.2f}–{item['p50_95_high_db']:.2f}"
        else:
            interval = "not bounded"
        count = min(item["trials"]) if item["trials"] else 0
        lines.append(
            f"| `{item['profile']}` | `{item['scenario']}` | {count} | {p50} | {interval} | "
            f"{item['frequency_spread_hz']:.2f} | {item['differential_delay_ms']:.2f} | "
            f"{item['scintillation_s4']:.2f}/{item['measured_s4']:.2f} | "
            f"{item['shadow_fade_db'] + item['fixed_loss_db']:.2f} |"
        )
    lines.extend(["", "## Per-point measurements", ""])
    for item in summaries:
        lines.extend([
            f"### {item['profile']} — {item['scenario']}",
            "",
            f"Reference: {item['reference']}. Model: {item['channel_model']}.",
            f"S4 target/measured: {item['scintillation_s4']:.3f}/{item['measured_s4']:.3f}; "
            f"phase sigma target/measured: {item['phase_sigma_rad']:.3f}/{item['measured_phase_sigma_rad']:.3f} rad; "
            f"P.681 tree-shadow quantile: {item['shadow_fade_db']:.2f} dB; fixed loss: {item['fixed_loss_db']:.2f} dB.",
            "",
            "| SNR 2500 (dB) | Successes / trials | Decode probability | Wilson 95% interval | PER |",
            "|---:|---:|---:|---:|---:|",
        ])
        for snr, success, trial, probability, interval in zip(
            item["snr_db"], item["successes"], item["trials"], item["probability"], item["wilson_95"]
        ):
            lines.append(
                f"| {snr:.1f} | {success}/{trial} | {probability:.3f} | "
                f"{interval[0]:.3f}–{interval[1]:.3f} | {1-probability:.3f} |"
            )
        lines.append("")
    lines.extend([
        "## Channel-model parameters and applicability", "",
        "| Family | Parameters in CI | Interpretation / validity boundary |",
        "|---|---|---|",
        "| QEX two-path | 0.1/0.5/1/10 Hz spread; 0.5/1/2/3 ms differential delay | Reproducible two-path stress model following the paper's equal-mean/equal-spread method; HF conditions are not a satellite orbit propagation claim. |",
        "| ITU-R P.531 scintillation | S4 = 0.2/0.45/0.8; nominal 0.1/0.5/1 Hz process | Nakagami-m intensity marginal and Gaussian phase proxy. Not full GISM and not a calibrated time-spectrum reproduction. |",
        "| ITU-R P.681 tree shadow | p=10%, elevation=45°, f=1.6 GHz, optical shadowing=65% | LMSS tree-shadow quantile equation within 0.8–20 GHz / elevation model limits. A separate -9 dB Rayleigh echo is synthetic test-only. Not a fixed high-gain ground-station model. |",
        "| ITU-R P.618 | fixed 3 dB at 2.4 GHz | Fade-margin stress only. No site coordinates, rain-rate, antenna elevation, polarization or local climatology are supplied; no full P.618 attenuation prediction is claimed. |",
        "",
        "## Race / concurrency regression", "",
        f"- Race detector status: **{race_status.upper()}**",
        "- Race tests are deliberately executed separately from the channel sweep so `-race` instrumentation does not bias throughput, latency or decode-probability measurements.",
        "",
    ])
    if race_results:
        lines.extend(f"- `{line}`" for line in race_results)
    else:
        lines.append("Race log was not available or contained no completed package lines.")
    lines.extend([
        "",
        "## References",
        "",
        "1. Steve Franke, K9AN, and Joe Taylor, K1JT, `FT4 and FT8`, QEX, July/August 2020, especially Figure 7 and Table 6 (QEX channel parameters and decode-probability methodology).",
        "2. ITU-R Recommendation F.1487, `Testing of HF modems with bandwidths of up to about 12 kHz using ionospheric channel simulators`.",
        "3. ITU-R P.531-16 (09/2025), `Ionospheric propagation data and prediction methods required for the design of satellite networks`.",
        "4. ITU-R P.681-11 (08/2019), `Propagation data required for the design of land mobile-satellite systems`.",
        "5. ITU-R P.618-14 (08/2023), `Propagation data and prediction methods required for the design of Earth-space telecommunication systems`.",
        "",
        "Official Recommendation pages: https://www.itu.int/rec/R-REC-P.531/en ; https://www.itu.int/rec/R-REC-P.681/en ; https://www.itu.int/rec/R-REC-P.618/en ; https://www.itu.int/rec/R-REC-F.1487/en .",
        "",
        "## Limitations and recommended follow-up",
        "",
        "- The M-ary and legacy decoders are exercised on deterministic synthetic PCM waveforms, not calibrated RF/IQ captures.",
        "- A single packet fixture per profile is suitable for CI regression detection but does not represent a statistically complete image/GIF transfer distribution.",
        "- The P.531, P.681 and P.618 entries are deliberately labelled as proxies/margin tests where the test harness lacks geographic, elevation, rain-rate, polarization and measured channel inputs required by the complete Recommendations.",
        "- P50 should be treated as a coarse simulation estimate. Increase trials and refine SNR spacing near 0.5 probability before publishing a product sensitivity specification.",
        "- Confirm operational performance with receiver-recorded WAV/IQ, calibrated RF level, realistic AGC, Doppler, oscillator error and satellite pass recordings.",
        "",
        "## Artifact files",
        "",
        "`channel-results.csv` contains the raw measurements; `channel-summary.json` contains the grouped probability curves and P50 estimates. PNG plots are included in the same workflow artifact.",
    ])
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--trials", type=int, default=int(os.getenv("SATSSDV_CHANNEL_TRIALS", "6")))
    parser.add_argument("--race-log", type=Path, default=None, help="optional combined Go race-detector log")
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    source_paths = sorted(args.input_dir.glob("*.csv"))
    rows = read_channel_csvs(source_paths)
    combined = args.output_dir / "channel-results.csv"
    write_combined_csv(rows, combined)
    summaries = aggregate_rows(rows)
    (args.output_dir / "channel-summary.json").write_text(json.dumps(summaries, indent=2), encoding="utf-8")
    plots = plot_probability_curves(summaries, args.output_dir)
    report = report_markdown(summaries, args)
    if not summaries:
        report += "\n**No channel CSV measurements were available.** See `channel-all-profiles.log` in the unified QEX/ITU report package for the failing test stage.\n"
    output = args.output_dir / "QEX-ITU-Channel-Sensitivity.md"
    output.write_text(report, encoding="utf-8")
    print(f"wrote QEX/ITU report ({len(rows)} rows, {len(plots)} plots): {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
