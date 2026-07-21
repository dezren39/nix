import { DEFAULT_WS_PATH } from "../constants.ts";

export function generateId(): string {
  return `exec_${Date.now()}_${Math.random().toString(36).substring(2, 9)}`;
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

export function wsUrlToHttpUrl(wsUrl: string): string {
  // Convert ws://localhost:3000/ws -> http://localhost:3000
  // Strip the WS path suffix (default "/ws") from the end
  return wsUrl
    .replace(/^ws:/, "http:")
    .replace(/^wss:/, "https:")
    .replace(new RegExp(`${escapeRegex(DEFAULT_WS_PATH)}$`), "");
}

import { removeSlashes } from "npm:slashes@3.0.12";

export async function readStdin(): Promise<string> {
  const raw = await new Response(Deno.stdin.readable).text();
  // Remove bash-escaped backslashes like \!
  return removeSlashes(raw);
}
