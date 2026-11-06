// The languages Go translates between; the codes match internal/languages.

export interface Language {
  code: string;
  name: string;
}

export const LANGUAGES: Language[] = [
  { code: "zh-HANS", name: "中文（简体）" },
  { code: "en", name: "英语" },
  { code: "ja", name: "日语" },
  { code: "ko", name: "韩语" },
  { code: "fr", name: "法语" },
  { code: "de", name: "德语" },
  { code: "ru", name: "俄语" },
  { code: "es", name: "西班牙语" },
  { code: "pt", name: "葡萄牙语" },
  { code: "it", name: "意大利语" },
];

/** langName names a code, or falls back to the code itself. */
export function langName(code: string): string {
  return LANGUAGES.find((l) => l.code === code)?.name ?? code;
}

/** The display name of a detected language; "自动检测" while unknown.
 * langdetect reports script-level codes ("zh" for Han, "latin" for any
 * Latin script), which map to their best display name; Latin is left
 * for the services to name. */
export function detectedName(code: string): string {
  switch (code) {
    case "":
    case "auto":
    case "latin":
    case "unknown":
      return "自动检测";
    case "zh":
      return langName("zh-HANS");
    default:
      return langName(code);
  }
}

/** How a detected code maps to a BCP-47 tag for speech synthesis. */
export function speechTag(code: string): string {
  if (code === "zh-HANS") return "zh-CN";
  return code;
}

/** The services' display names, by the codes Go uses. */
export const SERVICE_NAMES: Record<string, string> = {
  deepl: "DeepL",
  openai: "OpenAI",
  baidu: "百度",
  tencent: "腾讯云",
  youdao: "有道",
  azure: "微软翻译",
  google: "Google",
  dictionary: "词典",
};

/** The credentials each service asks for, by keyring name. */
export const SERVICE_CREDENTIALS: Record<string, { name: string; label: string; placeholder: string }[]> = {
  deepl: [{ name: "deepl-key", label: "API Key", placeholder: "DeepL-Auth-Key，免费版以 :fx 结尾" }],
  openai: [{ name: "openai-key", label: "API Key", placeholder: "sk-..." }],
  baidu: [
    { name: "baidu-appid", label: "APP ID", placeholder: "百度翻译开放平台" },
    { name: "baidu-key", label: "密钥", placeholder: "" },
  ],
  tencent: [
    { name: "tencent-secretid", label: "SecretId", placeholder: "访问管理的 API 密钥 Id" },
    { name: "tencent-secretkey", label: "SecretKey", placeholder: "" },
  ],
  youdao: [
    { name: "youdao-appkey", label: "应用 ID", placeholder: "有道智云应用 appKey" },
    { name: "youdao-secret", label: "应用密钥", placeholder: "" },
  ],
  azure: [
    { name: "azure-key", label: "订阅密钥", placeholder: "Translator 资源的 Key 1 或 Key 2" },
    { name: "azure-region", label: "区域", placeholder: "如 chinaeast2；全局资源留空" },
  ],
  google: [],
};

/** The services whose credentials can be applied for online; the button
 * opens the page Go keeps whitelisted. */
export const SERVICE_KEY_PAGES = new Set(["deepl", "openai", "baidu", "tencent", "youdao", "azure"]);
