import type { LogLevel, RealTimeLogLine } from "../types/logging";

const ANSI_RESET = "\x1b[0m";


const LEVEL_STYLE: Record<LogLevel, { ansi: string; label: string }> = {
  DEBUG: { ansi: "\x1b[90m", label: "DEBUG" },
  INFO: { ansi: "\x1b[32m", label: "INFO" },
  WARN: { ansi: "\x1b[33m", label: "WARN" },
  CRITICAL: { ansi: "\x1b[1;31m", label: "CRITICAL" },
};

const TS_COLOR = "\x1b[90m";
const NODE_COLOR = "\x1b[90m";

export function formatTimestamp(ts: number): string {
  const d = new Date(ts * 1000);
  return `${d.getHours().toString().padStart(2, "0")}:${d
    .getMinutes()
    .toString()
    .padStart(2, "0")}:${d.getSeconds().toString().padStart(2, "0")}.${d
    .getMilliseconds()
    .toString()
    .padStart(3, "0")}`;
}

export function formatLogLine(line: RealTimeLogLine): string {
  const lvl = LEVEL_STYLE[line.logLevel] ?? LEVEL_STYLE.INFO;
  const ts = formatTimestamp(line.timestamp);
  const paddedLevel = lvl.label.padEnd(8);
  return `${TS_COLOR}[${ts}]${ANSI_RESET} ${lvl.ansi}${paddedLevel}${ANSI_RESET} ${NODE_COLOR}${line.originNodeId}${ANSI_RESET} ${line.messagePayload}${ANSI_RESET}`;
}

export function stripAnsi(str: string): string {
  return str.replace(/\x1b\[[0-9;]*m/g, "");
}

export function lineToPlainText(line: RealTimeLogLine): string {
  const ts = formatTimestamp(line.timestamp);
  const msg = stripAnsi(line.messagePayload);
  return `[${ts}] ${line.logLevel.padEnd(8)} ${line.originNodeId} ${msg}`;
}
