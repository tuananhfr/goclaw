/**
 * Chứng minh: vé hết hạn thì mọi RPC chết, nhưng SOCKET VẪN MỞ.
 *
 * Đó là lý do client không bao giờ tự cứu mình — `onclose` không chạy nên
 * `scheduleReconnect()` cũng không chạy. Kết nối trông vẫn sống, mọi thứ hỏng.
 *
 * Dùng vé TTL ngắn để khỏi phải chờ 15 phút thật.
 *
 *   node test-ticket-expiry.mjs [ttl_giây]
 */
const GW = process.env.GOCLAW_GATEWAY_TOKEN ?? "";
const BASE = process.env.GOCLAW_BASE ?? "http://localhost:18790";
const TTL = parseInt(process.argv[2] ?? "60", 10);

const res = await fetch(`${BASE}/v1/agent-sessions`, {
  method: "POST",
  headers: { Authorization: `Bearer ${GW}`, "Content-Type": "application/json" },
  body: JSON.stringify({ user_id: "erpcons-28", agent_key: "erpcons", ttl_seconds: TTL }),
});
const ticket = await res.json();
if (!ticket.ok) {
  console.error("không đúc được vé:", ticket);
  process.exit(1);
}
console.log(`Vé TTL=${TTL}s, hết hạn lúc ${new Date(ticket.expires_at * 1000).toLocaleTimeString()}`);

const ws = new WebSocket(ticket.ws_url);
let seq = 0;
const pending = new Map();

function call(method, params) {
  const id = String(++seq);
  ws.send(JSON.stringify({ type: "req", id, method, params }));
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    setTimeout(() => pending.has(id) && reject(new Error("quá hạn phía client")), 30000);
  });
}

ws.onmessage = (e) => {
  const f = JSON.parse(e.data);
  if (f.type !== "res") return;
  const p = pending.get(f.id);
  if (!p) return;
  pending.delete(f.id);
  f.ok ? p.resolve(f.payload) : p.reject(new Error(`${f.error?.code}: ${f.error?.message}`));
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function probe(label) {
  const socketState = ["CONNECTING", "OPEN", "CLOSING", "CLOSED"][ws.readyState];
  try {
    await call("sessions.list", { agentId: "erpcons", limit: 1 });
    console.log(`  ${label}: RPC OK        | socket=${socketState}`);
  } catch (err) {
    console.log(`  ${label}: RPC HỎNG (${err.message}) | socket=${socketState}`);
  }
}

ws.onclose = () => console.log("  >>> socket ĐÓNG (onclose chạy -> client sẽ tự nối lại)");

ws.onopen = async () => {
  await call("connect", { token: ticket.token, user_id: ticket.user_id, locale: "vi" });
  console.log("Đã nối.\n");

  await probe("ngay sau khi nối ");
  console.log(`\nChờ ${TTL + 8}s cho vé hết hạn...\n`);
  await sleep((TTL + 8) * 1000);
  await probe("sau khi vé hết hạn");

  console.log(
    `\nsocket = ${["CONNECTING", "OPEN", "CLOSING", "CLOSED"][ws.readyState]}` +
      " -> RPC hỏng mà socket vẫn mở thì client không có cách nào biết để tự nối lại.\n",
  );

  // Vé đã CHẾT thì không chữa tại chỗ được, kể cả sau khi `connect` đã được đưa
  // vào allowlist: phép kiểm hạn chạy TRƯỚC khi xét method. Đây là ranh giới,
  // không phải lỗi - client buộc phải dựng lại socket.
  console.log("Thử gia hạn tại chỗ bằng vé mới (dự kiến: BỊ TỪ CHỐI vì vé cũ đã chết)...");
  const fresh = await (
    await fetch(`${BASE}/v1/agent-sessions`, {
      method: "POST",
      headers: { Authorization: `Bearer ${GW}`, "Content-Type": "application/json" },
      body: JSON.stringify({ user_id: "erpcons-28", agent_key: "erpcons", ttl_seconds: TTL }),
    })
  ).json();

  try {
    await call("connect", { token: fresh.token, user_id: fresh.user_id, locale: "vi" });
    console.log("  gia hạn ĂN (ngoài dự kiến - phép kiểm hạn đã đổi?)");
  } catch (err) {
    console.log(`  bị từ chối như dự kiến: ${err.message}`);
    console.log("  -> client phải đóng socket và nối lại. Xem `renewTicket()` trong ws-client.ts.");
  }

  console.log("\nGia hạn TRƯỚC khi hết hạn thì ăn — chạy `test-ticket-renew.mjs` để thấy.");
  process.exit(0);
};
