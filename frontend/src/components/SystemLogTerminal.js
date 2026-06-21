import { jsxs as _jsxs, jsx as _jsx } from "react/jsx-runtime";
import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";
import { formatLogLine, lineToPlainText } from "../utils/ansiParser";
export function SystemLogTerminal({ incomingLogStream, onTerminalReady, }) {
    const containerRef = useRef(null);
    const terminalRef = useRef(null);
    const fitAddonRef = useRef(null);
    const isAtBottomRef = useRef(true);
    const lineOffsetRef = useRef(0);
    const lineCountRef = useRef(0);
    const pausedLinesRef = useRef([]);
    const [isPaused, setIsPaused] = useState(false);
    const [lineCount, setLineCount] = useState(0);
    const [isReady, setIsReady] = useState(false);
    useEffect(() => {
        if (!containerRef.current)
            return;
        const terminal = new Terminal({
            theme: {
                background: "#09090b",
                foreground: "#e4e4e7",
                cursor: "#22d3ee",
                selectionBackground: "#22d3ee33",
                black: "#18181b",
                red: "#f43f5e",
                green: "#22c55e",
                yellow: "#eab308",
                blue: "#3b82f6",
                magenta: "#a78bfa",
                cyan: "#22d3ee",
                white: "#e4e4e7",
                brightBlack: "#52525b",
                brightRed: "#f43f5e",
                brightGreen: "#22c55e",
                brightYellow: "#facc15",
                brightBlue: "#60a5fa",
                brightMagenta: "#c084fc",
                brightCyan: "#67e8f9",
                brightWhite: "#f4f4f5",
            },
            fontFamily: '"JetBrains Mono", "Fira Code", Consolas, monospace',
            fontSize: 13,
            cursorBlink: true,
            allowTransparency: true,
        });
        const fitAddon = new FitAddon();
        terminal.loadAddon(fitAddon);
        fitAddonRef.current = fitAddon;
        terminal.open(containerRef.current);
        fitAddon.fit();
        terminal.onScroll(() => {
            const buffer = terminal.buffer.active;
            const atBottom = buffer.viewportY + terminal.rows >= buffer.length;
            isAtBottomRef.current = atBottom;
        });
        terminalRef.current = terminal;
        setIsReady(true);
        onTerminalReady?.(terminal);
        return () => {
            terminal.dispose();
            terminalRef.current = null;
            setIsReady(false);
        };
    }, []);
    useEffect(() => {
        if (!containerRef.current || !fitAddonRef.current)
            return;
        let timer;
        const observer = new ResizeObserver(() => {
            clearTimeout(timer);
            timer = setTimeout(() => fitAddonRef.current?.fit(), 100);
        });
        observer.observe(containerRef.current);
        return () => {
            observer.disconnect();
            clearTimeout(timer);
        };
    }, [isReady]);
    useEffect(() => {
        const terminal = terminalRef.current;
        if (!terminal)
            return;
        const offset = lineOffsetRef.current;
        const lines = incomingLogStream;
        if (offset >= lines.length)
            return;
        for (let i = offset; i < lines.length; i++) {
            const formatted = formatLogLine(lines[i]);
            if (isPaused) {
                pausedLinesRef.current.push(formatted);
                lineOffsetRef.current = i + 1;
                continue;
            }
            const yBefore = terminal.buffer.active.viewportY;
            terminal.writeln(formatted);
            if (!isAtBottomRef.current) {
                terminal.scrollToLine(yBefore);
            }
            lineCountRef.current += 1;
            lineOffsetRef.current = i + 1;
        }
        setLineCount(lineCountRef.current);
    }, [incomingLogStream, isPaused]);
    function handleClear() {
        terminalRef.current?.clear();
        lineCountRef.current = 0;
        setLineCount(0);
        pausedLinesRef.current = [];
    }
    const handleTogglePause = useCallback(() => {
        setIsPaused((prev) => {
            if (prev) {
                const terminal = terminalRef.current;
                if (terminal && pausedLinesRef.current.length > 0) {
                    for (const l of pausedLinesRef.current) {
                        const yBefore = terminal.buffer.active.viewportY;
                        terminal.writeln(l);
                        if (!isAtBottomRef.current) {
                            terminal.scrollToLine(yBefore);
                        }
                        lineCountRef.current += 1;
                    }
                    setLineCount(lineCountRef.current);
                    pausedLinesRef.current = [];
                }
            }
            return !prev;
        });
    }, []);
    const handleExport = useCallback(() => {
        const content = incomingLogStream.map(lineToPlainText).join("\n");
        const blob = new Blob([content], { type: "text/plain" });
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = `logs-${Date.now()}.log`;
        anchor.click();
        URL.revokeObjectURL(url);
    }, [incomingLogStream]);
    return (_jsxs("div", { className: "flex flex-col h-full bg-zinc-950", children: [_jsxs("div", { className: "flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 shrink-0", children: [_jsxs("div", { className: "flex items-center gap-4", children: [_jsxs("span", { className: "text-[11px] font-mono text-zinc-500", children: [lineCount, " lines"] }), _jsx("span", { className: `inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold ${isPaused
                                    ? "bg-amber-900/40 text-amber-400"
                                    : "bg-emerald-900/40 text-emerald-400"}`, children: isPaused ? "PAUSED" : "LIVE" })] }), _jsxs("div", { className: "flex items-center gap-1.5", children: [_jsx("button", { onClick: handleClear, className: "px-2 py-0.5 rounded text-[11px] font-medium text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors", children: "Clear" }), _jsx("button", { onClick: handleTogglePause, className: "px-2 py-0.5 rounded text-[11px] font-medium text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors", children: isPaused ? "Resume" : "Pause" }), _jsx("button", { onClick: handleExport, className: "px-2 py-0.5 rounded text-[11px] font-medium text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors", children: "Export" })] })] }), _jsx("div", { ref: containerRef, className: "flex-1 overflow-hidden" })] }));
}
