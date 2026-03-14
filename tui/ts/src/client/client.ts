import net from "node:net";
import { EventEmitter } from "node:events";
import readline from "node:readline";

const SOCKET_PATH = "/tmp/csm-ctl.sock";

export interface PendingTool {
  tool_name: string;
  tool_input: Record<string, unknown>;
  safety: "safe" | "destructive" | "unknown";
}

export interface Activity {
  timestamp: string;
  activity_type: string;
  summary: string;
}

export interface Session {
  session_id: string;
  slug: string;
  cwd: string;
  project_name: string;
  state: "running" | "waiting" | "idle" | "dead";
  autopilot: boolean;
  has_destructive: boolean;
  pending_tools: PendingTool[];
  ghostty_tab: string;
  git_branch: string;
  last_text: string;
  activities: Activity[];
  last_activity_time: string;
  pid: number;
}

interface SessionsUpdatedEvent {
  event: "sessions_updated";
  sessions: Session[];
}

interface OkResponse {
  ok: boolean;
  autopilot?: boolean;
}

type ServerMessage = SessionsUpdatedEvent | OkResponse;

export interface CSMClient {
  on(event: "sessions", listener: (sessions: Session[]) => void): this;
  on(event: "error", listener: (err: Error) => void): this;
  on(event: "close", listener: () => void): this;
}

export class CSMClient extends EventEmitter {
  private socket: net.Socket | null = null;
  private rl: readline.Interface | null = null;

  connect(): void {
    this.socket = net.createConnection(SOCKET_PATH);

    this.socket.on("connect", () => {
      this.send({ action: "subscribe" });
    });

    this.rl = readline.createInterface({ input: this.socket });

    this.rl.on("line", (line: string) => {
      if (!line.trim()) return;
      try {
        const msg: ServerMessage = JSON.parse(line);
        if ("event" in msg && msg.event === "sessions_updated") {
          this.emit("sessions", msg.sessions);
        }
      } catch {
        // ignore malformed lines
      }
    });

    this.socket.on("error", (err: Error) => {
      this.emit("error", err);
    });

    this.socket.on("close", () => {
      this.emit("close");
    });
  }

  private send(obj: Record<string, unknown>): void {
    this.socket?.write(JSON.stringify(obj) + "\n");
  }

  toggleAutopilot(sessionId: string): void {
    this.send({ action: "toggle_autopilot", session_id: sessionId });
  }

  approve(sessionId: string): void {
    this.send({ action: "approve", session_id: sessionId });
  }

  reject(sessionId: string): void {
    this.send({ action: "reject", session_id: sessionId });
  }

  focus(sessionId: string): void {
    this.send({ action: "focus", session_id: sessionId });
  }

  disconnect(): void {
    this.rl?.close();
    this.socket?.destroy();
    this.socket = null;
    this.rl = null;
  }
}
