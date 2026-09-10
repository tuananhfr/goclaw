/**
 * Test đầu-cuối: hỏi trợ lý một câu cần dữ liệu ERPcons, xem nó có gọi tool và
 * trả lời bằng số thật không.
 *
 * Đi đúng đường của trình duyệt: đúc vé -> WebSocket -> `chat.send`. Khác duy
 * nhất là vé đúc thẳng từ GoClaw bằng gateway token thay vì qua Drupal, để test
 * chạy được mà không cần cookie phiên.
 *
 * Dùng:
 *   node test-chat.mjs <agent_key> <uid Drupal> "<câu hỏi>"
 */
const GW = process.env.GOCLAW_GATEWAY_TOKEN ?? "";
const BASE = process.env.GOCLAW_BASE ?? "http://localhost:18790";

const [agentKey, uid, question] = process.argv.slice(2);
if (!agentKey || !uid || !question) {
  console.error('Dùng: node test-chat.mjs <agent_key> <uid> "<câu hỏi>"');
  process.exit(2);
}

const ticketRes = await fetch(`${BASE}/v1/agent-sessions`, {
  method: "POST",
  headers: { Authorization: `Bearer ${GW}`, "Content-Type": "application/json" },
  body: JSON.stringify({ user_id: `erpcons-${uid}`, agent_key: agentKey, ttl_seconds: 900 }),
});
const ticket = await ticketRes.json();
if (!ticket.ok) {
  console.error("Không đúc được vé:", ticket);
  process.exit(1);
}

// GoClaw dựng ws_url từ Host của request đúc vé; ở đây request đi từ host nên
// giá trị trả về đã dùng được, không cần public_ws_url.
const ws = new WebSocket(ticket.ws_url);
let seq = 0;
const pending = new Map();
const toolCalls = [];

function call(method, params) {
  const id = String(++seq);
  ws.send(JSON.stringify({ type: "req", id, method, params }));
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    setTimeout(() => pending.has(id) && reject(new Error(`${method} quá hạn`)), 180000);
  });
}

ws.onmessage = (event) => {
  const frame = JSON.parse(event.data);

  if (frame.type === "res") {
    const entry = pending.get(frame.id);
    if (!entry) return;
    pending.delete(frame.id);
    frame.ok ? entry.resolve(frame.payload) : entry.reject(new Error(frame.error?.message ?? "lỗi"));
    return;
  }

  if (frame.type === "event" && frame.event === "agent") {
    const p = frame.payload ?? {};
    if (process.env.DUMP_EVENTS === "1" && p.type !== "chunk") {
      console.log(`  [event] ${p.type} ${JSON.stringify(p.payload ?? {}).slice(0, 100)}`);
    }
    // Bằng chứng agent thực sự gọi tool chứ không trả lời từ trí nhớ.
    // Tên sự kiện ở `pkg/protocol/events.go`; payload của `tool.call` mang
    // `{name, id, arguments}` (loop_pipeline_tool_callbacks.go:31).
    if (p.type === "tool.call") {
      const name = p.payload?.name ?? "?";
      const args = JSON.stringify(p.payload?.arguments ?? {});
      toolCalls.push(name);
      console.log(`  [tool.call] ${name} ${args.slice(0, 120)}`);
    }
    if (p.type === "tool.result") {
      const preview = JSON.stringify(p.payload ?? {}).slice(0, 160);
      console.log(`  [tool.result] ${preview}`);
    }
  }
};

ws.onerror = (err) => {
  console.error("Lỗi WebSocket:", err.message ?? err);
  process.exit(1);
};

ws.onopen = async () => {
  try {
    await call("connect", { token: ticket.token, user_id: ticket.user_id, locale: "vi" });
    console.log(`Đã nối. agent=${agentKey} user=${ticket.user_id}`);
    console.log(`Hỏi: ${question}\n`);

    const sessionKey = `agent:${agentKey}:ws:direct:${crypto.randomUUID()}`;
    const started = Date.now();
    const res = await call("chat.send", { agentId: agentKey, sessionKey, message: question });

    console.log(`\n--- Trả lời (${((Date.now() - started) / 1000).toFixed(1)}s) ---`);
    console.log(res?.content ?? "(rỗng)");
    console.log(`\nTool đã gọi: ${toolCalls.length ? toolCalls.join(", ") : "(KHÔNG GỌI TOOL NÀO)"}`);
    process.exit(0);
  } catch (err) {
    console.error("Hỏng:", err.message);
    process.exit(1);
  }
};
