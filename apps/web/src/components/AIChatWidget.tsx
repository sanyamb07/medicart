import { useRef, useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { MessageCircle, Send, X, Bot, AlertTriangle } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";

interface ChatMessage {
  id: number;
  role: "user" | "assistant";
  text: string;
  escalate?: boolean;
  links?: { label: string; href: string }[];
}

interface AssistantReply {
  text: string;
  suggestions?: string[];
  escalate: boolean;
  links?: { label: string; href: string }[];
}

let nextID = 1;

const WELCOME: ChatMessage = {
  id: 0,
  role: "assistant",
  text: "Hi! I can check product availability and pricing, compare brands with the same composition, check delivery to a PIN code, and look up an order. I can't advise on dosage, switching medicines or other medical questions — a pharmacist handles those.",
};

export default function AIChatWidget() {
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([WELCOME]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [messages, open]);

  async function send(text: string) {
    const trimmed = text.trim();
    if (!trimmed || sending) return;
    setMessages((m) => [...m, { id: nextID++, role: "user", text: trimmed }]);
    setInput("");
    setSending(true);
    try {
      const res = await api.post<Envelope<AssistantReply>>("/api/v1/assistant/messages", { message: trimmed });
      setMessages((m) => [...m, { id: nextID++, role: "assistant", text: res.data.text, escalate: res.data.escalate, links: res.data.links }]);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Something went wrong. Please try again.";
      setMessages((m) => [...m, { id: nextID++, role: "assistant", text: msg, escalate: true }]);
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="fixed bottom-20 right-4 z-40 md:bottom-6 md:right-6" style={{ paddingBottom: "env(safe-area-inset-bottom, 0px)" }}>
      {open && (
        <div className="mb-3 w-[min(92vw,360px)] h-[min(70vh,480px)] bg-white border border-slate-200 rounded-2xl shadow-xl flex flex-col overflow-hidden">
          <div className="bg-brand-600 text-white px-4 py-3 flex items-center justify-between shrink-0">
            <div className="flex items-center gap-2">
              <Bot size={18} />
              <div>
                <div className="text-sm font-semibold leading-tight">MediCart Assistant</div>
                <div className="text-[11px] text-brand-100 leading-tight">Rule-based demo assistant — not a live AI</div>
              </div>
            </div>
            <button onClick={() => setOpen(false)} aria-label="Close chat" className="text-brand-100 hover:text-white">
              <X size={18} />
            </button>
          </div>

          <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-3 space-y-2 bg-slate-50">
            {messages.map((m) => (
              <div key={m.id} className={`flex ${m.role === "user" ? "justify-end" : "justify-start"}`}>
                <div
                  className={`max-w-[85%] text-sm rounded-2xl px-3 py-2 whitespace-pre-line ${
                    m.role === "user"
                      ? "bg-brand-600 text-white rounded-br-sm"
                      : m.escalate
                        ? "bg-amber-50 text-amber-900 border border-amber-200 rounded-bl-sm"
                        : "bg-white text-slate-700 border border-slate-200 rounded-bl-sm"
                  }`}
                >
                  {m.escalate && m.role === "assistant" && (
                    <div className="flex items-center gap-1 text-amber-600 text-[11px] font-semibold mb-1">
                      <AlertTriangle size={12} /> Needs a person
                    </div>
                  )}
                  {m.text}
                  {m.links && m.links.length > 0 && (
                    <span className="mt-2 flex flex-col gap-1.5">
                      {m.links.map((l) => (
                        <Link
                          key={l.href}
                          to={l.href}
                          onClick={() => setOpen(false)}
                          className="inline-flex items-center justify-center rounded-lg bg-brand-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-brand-700"
                        >
                          {l.label}
                        </Link>
                      ))}
                    </span>
                  )}
                </div>
              </div>
            ))}
            {sending && <div className="text-xs text-slate-400 px-1">Thinking…</div>}
          </div>

          <form
            onSubmit={(e) => {
              e.preventDefault();
              void send(input);
            }}
            className="border-t border-slate-200 p-2 flex items-center gap-2 shrink-0 bg-white"
          >
            <input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Ask about stock, delivery, or an order…"
              className="flex-1 text-sm px-3 py-2 rounded-full border border-slate-200 focus:outline-none focus:ring-2 focus:ring-brand-200"
            />
            <button
              type="submit"
              disabled={sending || !input.trim()}
              aria-label="Send"
              className="bg-brand-600 text-white rounded-full p-2 disabled:opacity-40"
            >
              <Send size={16} />
            </button>
          </form>
        </div>
      )}

      <button
        onClick={() => setOpen((o) => !o)}
        aria-label={open ? "Close assistant" : "Open assistant"}
        className="bg-brand-600 text-white rounded-full p-3.5 shadow-lg hover:bg-brand-700 transition-colors"
      >
        {open ? <X size={22} /> : <MessageCircle size={22} />}
      </button>
    </div>
  );
}
