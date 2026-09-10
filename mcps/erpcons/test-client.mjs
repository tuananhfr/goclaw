/**
 * Client MCP tối giản để thử sidecar bằng tay.
 *
 * Không phải test tự động (repo GoClaw dùng `go test`, không có runner cho JS ở
 * đây) — đây là công cụ chẩn đoán: nó nói cho biết một token cụ thể nhìn thấy
 * những tool nào và gọi ra dữ liệu gì, tức đúng câu hỏi hay phải trả lời khi
 * "trợ lý bảo không có quyền".
 *
 * Dùng:
 *   node test-client.mjs <url sse> <token> [tên_tool] [json tham số]
 *
 * Ví dụ:
 *   node test-client.mjs http://localhost:3399/sse erpa_xxx
 *   node test-client.mjs http://localhost:3399/sse erpa_xxx erp_tasks '{"limit":3}'
 */
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { SSEClientTransport } from "@modelcontextprotocol/sdk/client/sse.js";

const [url, token, toolName, rawArgs] = process.argv.slice(2);

if (!url || !token) {
  console.error("Dùng: node test-client.mjs <url sse> <token> [tool] [json]");
  process.exit(2);
}

// Token đi ở header của KẾT NỐI, giống hệt cách GoClaw gắn credential per-user.
const transport = new SSEClientTransport(new URL(url), {
  requestInit: { headers: { "X-ERP-Assistant-Token": token } },
  eventSourceInit: {
    fetch: (input, init) =>
      fetch(input, {
        ...init,
        headers: { ...(init?.headers ?? {}), "X-ERP-Assistant-Token": token },
      }),
  },
});

const client = new Client({ name: "erpcons-test-client", version: "1.0.0" }, { capabilities: {} });
await client.connect(transport);

const { tools } = await client.listTools();
console.log(`\nThấy ${tools.length} tool:`);
for (const tool of tools) {
  const params = Object.keys(tool.inputSchema?.properties ?? {});
  console.log(`  ${tool.name.padEnd(18)} (${params.length} tham số) ${tool.description.slice(0, 70).replace(/\s+/g, " ")}…`);
}

if (toolName) {
  const args = rawArgs ? JSON.parse(rawArgs) : {};
  console.log(`\nGọi ${toolName} ${JSON.stringify(args)}`);
  const result = await client.callTool({ name: toolName, arguments: args });
  const text = result.content?.[0]?.text ?? "";
  console.log(`isError=${result.isError === true}  ${text.length} ký tự`);
  console.log(text.slice(0, 1200));
}

// Thoát thẳng, KHÔNG `client.close()`: đóng transport SSE rồi exit ngay làm
// libuv trên Windows bắn assert `!(handle->flags & UV_HANDLE_CLOSING)`. Đây là
// tiến trình dùng một lần nên bỏ qua bước dọn là hợp lý.
process.exit(0);
