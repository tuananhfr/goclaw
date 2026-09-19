import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { ErpClient, ErpRequestError, ToolInputError } from "./erp-client.js";
import { tokenize } from "./text.js";

/** Logo lớn nhất đang có là ~800KB; trần này chặn việc kéo cả tệp ảnh khổng lồ vào phiên. */
const MAX_LOGO_BYTES = 3 * 1024 * 1024;

type BrandingContent =
  | { type: "text"; text: string }
  | { type: "image"; data: string; mimeType: string; annotations: { audience: ("user" | "assistant")[] } };

interface CompanyRow {
  id: number;
  name: string;
  abbreviation: string;
  hasLogo: boolean;
}

// 200 byte đầu được GoClaw chép vào system prompt, nên luật "cấm tự vẽ logo"
// phải đứng đầu — agent cần thấy nó cả khi chưa nghĩ tới việc gọi tool này.
const DESCRIPTION = [
  "Lấy logo THẬT + letterhead công ty. BẮT BUỘC trước khi tạo ảnh/PDF/Word/slide có nhắc tới công ty; cấm tự vẽ hay dùng AI sinh logo.",
  "",
  "Chọn công ty theo nội dung (dữ liệu thuộc công ty nào thì dùng công ty đó); bỏ trống `company` = công ty chính của người hỏi. Chỉ lấy được công ty người hỏi thuộc về.",
  "Ảnh logo được lưu vào workspace; đường dẫn nằm trong kết quả. Dùng nguyên ảnh, giữ tỉ lệ, không cắt, không đổi màu:",
  "- Word (python-docx): đặt vào header: section.header.paragraphs[0].add_run().add_picture(path, height=Cm(1.5)).",
  "- PDF (reportlab): canvas.drawImage(path, x, y, height=..., preserveAspectRatio=True, mask='auto'). Chữ tiếng Việt PHẢI đăng ký font TTF: pdfmetrics.registerFont(TTFont('DejaVu', '/app/data/fonts/DejaVuSans.ttf')) (có cả DejaVuSans-Bold.ttf); font mặc định mất dấu.",
  "- Slide (python-pptx): slide.shapes.add_picture(path, left, top, height=...).",
  "- Ảnh (create_image): sinh ảnh KHÔNG có logo (deliver=false, chừa khoảng trống), rồi dán logo thật bằng PIL (Image.alpha_composite / paste có mask), sau đó gửi file bằng send_file. Không đưa logo làm ảnh tham chiếu cho create_image: model sẽ vẽ lại và làm méo. Viết chữ tiếng Việt bằng PIL thì dùng ImageFont.truetype('/app/data/fonts/DejaVuSans.ttf', size).",
  "Letterhead (tên, MST, địa chỉ, SĐT, email, website, footer) lấy từ kết quả, không tự bịa.",
].join("\n");

/** Đăng ký `erp_company_branding` cho kết nối của MỘT người. */
export function registerBrandingTool(server: McpServer, client: ErpClient): void {
  server.tool(
    "erp_company_branding",
    DESCRIPTION,
    {
      company: z.union([z.number(), z.string()]).optional().describe(
        "ID công ty, hoặc tên/tên viết tắt (vd: Tekshot, TKS). Bỏ trống = công ty chính của người hỏi.",
      ),
    },
    async ({ company }: { company?: number | string }) => {
      try {
        const id = await resolveCompany(client, company);
        const body = await client.fetchBranding(id);
        const info = (body.company ?? {}) as Record<string, unknown>;
        const logo = info.logo as { path?: string } | null | undefined;

        const content: BrandingContent[] = [];
        let logoNote = "Công ty này CHƯA có logo trong ERPcons. Chỉ dùng tên công ty dạng chữ, không tự vẽ logo.";
        if (logo?.path) {
          // Tải logo hỏng KHÔNG được làm hỏng cả tool: báo lỗi chung thì mô hình
          // hiểu thành "không có công ty này" rồi đi xin người dùng cả MST, địa
          // chỉ — trong khi letterhead vẫn đọc được.
          try {
            const image = await client.fetchSiteImage(logo.path, MAX_LOGO_BYTES);
            content.push({
              type: "image" as const,
              data: image.data.toString("base64"),
              mimeType: image.mimeType,
              // Tài nguyên cho agent dùng, không phải thứ để gửi cho người dùng:
              // GoClaw lưu vào workspace nhưng không đính kèm vào khung chat.
              annotations: { audience: ["assistant"] },
            });
            logoNote = "Đã lưu vào workspace — dùng đường dẫn ảnh trong kết quả này.";
          } catch (err) {
            logoNote = `Không tải được tệp logo (${describeError(err)}). Dùng tên công ty dạng chữ, KHÔNG tự vẽ logo, và báo người dùng logo đang lỗi.`;
          }
        }
        const { logo: _logo, ...letterhead } = info;
        content.unshift({
          type: "text" as const,
          text: JSON.stringify({ company: letterhead, logo: logoNote }),
        });
        return { content };
      } catch (err) {
        return {
          isError: true,
          content: [{ type: "text" as const, text: describeError(err) }],
        };
      }
    },
  );
}

/**
 * Tên -> ID trong phạm vi của người hỏi.
 *
 * Mô hình hay đưa tên ("Tekshot", "TKS") thay vì ID. Khớp mơ hồ thì trả danh
 * sách để nó hỏi lại người dùng, không đoán bừa công ty in lên tài liệu.
 */
export async function resolveCompany(client: Pick<ErpClient, "fetchBranding">, company: number | string | undefined): Promise<number> {
  if (typeof company === "number" || (typeof company === "string" && /^\d+$/.test(company.trim()))) {
    return Number(company);
  }

  const list = await client.fetchBranding();
  const items = (list.items ?? []) as CompanyRow[];
  const query = typeof company === "string" ? tokenize(company) : [];

  if (query.length === 0) {
    const fallback = typeof list.default === "number" ? list.default : items.length === 1 ? items[0].id : null;
    if (fallback !== null) return fallback;
    throw new ToolInputError(`Người hỏi không có công ty chính. Hỏi lại họ muốn dùng công ty nào: ${describeChoices(items)}`);
  }

  const matches = items.filter((row) => {
    const haystack = ` ${tokenize(`${row.name} ${row.abbreviation}`).join(" ")} `;
    return query.every((token) => haystack.includes(` ${token} `));
  });
  if (matches.length === 1) return matches[0].id;
  if (matches.length === 0) {
    throw new ToolInputError(`Không có công ty "${company}" trong phạm vi của người hỏi. Công ty dùng được: ${describeChoices(items)}`);
  }
  throw new ToolInputError(`"${company}" khớp nhiều công ty, hỏi lại người dùng: ${describeChoices(matches)}`);
}

function describeChoices(rows: CompanyRow[]): string {
  return rows.map((row) => `${row.id} = ${row.name}${row.abbreviation ? ` (${row.abbreviation})` : ""}`).join("; ") || "(không có)";
}

function describeError(err: unknown): string {
  if (err instanceof ToolInputError || err instanceof ErpRequestError) return err.message;
  return "Không lấy được nhận diện công ty từ ERPcons.";
}
