export type LogLevel = "DEBUG" | "INFO" | "WARN" | "CRITICAL";

export interface RealTimeLogLine {
  id: string;
  timestamp: number;
  originNodeId: string;
  logLevel: LogLevel;
  messagePayload: string;
}

export interface SystemLogTerminalProps {
  tenantId: string;
  activeTargetNodeId: string;
  maxBufferLinesCount?: number;
  incomingLogStream: RealTimeLogLine[];
  onTerminalReady?: (terminalInstance: unknown) => void;
}

export const DEFAULT_MAX_BUFFER_LINES = 2500;

