/**
 * 服务器地区（ISO 3166-1 alpha-2）。管理员不选时由面板按服务器地址自动识别
 * （DB-IP 免费国家库），状态页显示对应的国旗。
 */

// 常见的服务器所在地，按大致地理顺序；列表外的代码（自动识别出的冷门地区）也能显示。
export const NODE_REGION_CODES = [
  "CN",
  "HK",
  "MO",
  "TW",
  "JP",
  "KR",
  "SG",
  "MY",
  "TH",
  "VN",
  "PH",
  "ID",
  "IN",
  "AE",
  "TR",
  "IL",
  "RU",
  "UA",
  "KZ",
  "DE",
  "NL",
  "GB",
  "FR",
  "IE",
  "IT",
  "ES",
  "PT",
  "CH",
  "AT",
  "BE",
  "SE",
  "NO",
  "FI",
  "DK",
  "PL",
  "CZ",
  "RO",
  "BG",
  "LU",
  "US",
  "CA",
  "MX",
  "BR",
  "AR",
  "CL",
  "AU",
  "NZ",
  "ZA",
  "EG",
  "NG",
];

let regionNames: Intl.DisplayNames | null | undefined;

/** 地区代码的中文名（浏览器不支持时显示代码本身）。 */
export function nodeRegionName(code: string): string {
  if (!code) return "";
  if (regionNames === undefined) {
    try {
      regionNames = new Intl.DisplayNames(["zh-CN"], { type: "region" });
    } catch {
      regionNames = null;
    }
  }
  try {
    return regionNames?.of(code) || code;
  } catch {
    return code;
  }
}

/** 国旗 emoji，例如 "HK" → 🇭🇰。 */
export function nodeRegionFlag(code: string): string {
  if (!/^[A-Z]{2}$/.test(code)) return "";

  return String.fromCodePoint(
    ...[...code].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65),
  );
}

export function nodeRegionLabel(code: string): string {
  if (!code) return "";

  return `${nodeRegionFlag(code)} ${nodeRegionName(code)}`.trim();
}

/** 下拉框里的选项：常见地区，加上当前值和识别值（如果不在常见列表里）。 */
export function nodeRegionOptions(...extra: string[]): string[] {
  const codes = [...NODE_REGION_CODES];

  for (const code of extra) {
    if (code && !codes.includes(code)) codes.push(code);
  }

  return codes;
}
