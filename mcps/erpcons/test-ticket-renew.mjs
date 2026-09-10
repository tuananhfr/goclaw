/**
 * Gia hạn vé TRƯỚC khi hết hạn, trên chính socket đang mở.
 *
 * Phân biệt với `test-ticket-expiry.mjs`: ở đó vé đã chết rồi mới chữa, và
 * KHÔNG chữa được - cổng `authorizeAgentSessionRPC` chạy trước handler nên chặn
 * luôn cả `connect`. Ở đây chữa lúc vé còn sống, là đường đi thật của client.
 *
 *   node test-ticket-renew.mjs [ttl_giây]
 */
const GW = process.env.GOCLAW_GATEWAY_TOKEN ?? "";
const BASE = process.env.GOCLAW_BASE ?? "http://localhost:18790";
const TTL = parseInt(process.argv[2] ?? "60", 10);

const mint = async () => {
  const r = await fetch(`${BASE}/v1/agent-sessions`, {
    method: "POST",
    headers: { Authorization: `Bearer ${GW}`, "Content-Type": "application/json" },
    body: JSON.stringify({ user_id: "erpcons-28", agent_key: "erpcons", ttl_seconds: TTL }),
  });
  return r.json();
};

const first = await mint();
console.log(`Vé 1: TTL=${TTL}s, hết hạn ${new Date(first.expires_at * 1000).toLocaleTimeString()}`);

const ws = new WebSocket(first.ws_url);
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
  try {
    await call("sessions.list", { agentId: "erpcons", limit: 1 });
    console.log(`  ${label}: RPC OK`);
    return true;
  } catch (err) {
    console.log(`  ${label}: RPC HỎNG (${err.message})`);
    return false;
  }
}

ws.onopen = async () => {
  await call("connect", { token: first.token, user_id: first.user_id, locale: "vi" });
  console.log("Đã nối.\n");
  await probe("mốc 0s            ");

  // Client thật gia hạn ở 80% TTL. Ở đây làm sớm hơn một chút cho chắc nhịp.
  const renewAt = Math.floor(TTL * 0.6);
  console.log(`\nChờ ${renewAt}s rồi gia hạn (vé CÒN sống)...`);
  await sleep(renewAt * 1000);

  const second = await mint();
  await call("connect", { token: second.token, user_id: second.user_id, locale: "vi" });
  console.log(
    `  đã thay sang vé 2, hết hạn ${new Date(second.expires_at * 1000).toLocaleTimeString()}` +
      "  (socket KHÔNG dựng lại)\n",
  );

  // Vượt qua mốc hết hạn của vé 1. Nếu gia hạn ăn thì RPC vẫn phải chạy.
  const past = TTL - renewAt + 8;
  console.log(`Chờ thêm ${past}s — quá hạn của VÉ 1...`);
  await sleep(past * 1000);

  const ok = await probe("sau hạn của vé 1  ");
  console.log(
    `\nKẾT LUẬN: ${ok ? "GIA HẠN TẠI CHỖ ĂN — không cần dựng lại socket." : "gia hạn KHÔNG ăn."}`,
  );
  process.exit(ok ? 0 : 1);
};
