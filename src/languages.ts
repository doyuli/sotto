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
  siliconflow: "硅基流动",
  zhipu: "智谱",
  baidu: "百度翻译",
  volcengine: "火山引擎",
  tencent: "腾讯云",
  youdao: "有道",
  azure: "微软翻译",
  google: "Google",
  "index-translate": "Bilibili",
  dictionary: "词典",
};

/** Optional notes shown in the translation service settings. */
export const SERVICE_NOTES: Partial<Record<string, string>> = {
  siliconflow: "免费模型 tencent/Hunyuan-MT-7B；需实名认证并创建 API Key，调用受平台限流。",
  zhipu: "使用免费模型 glm-4.7-flash；请在智谱开放平台创建 API Key，调用受平台限流。",
  baidu: "凭证来自百度翻译开放平台的「开发者信息」；使用前需开通「通用文本翻译」。",
  volcengine: "凭证来自火山引擎访问控制；使用前需开通「机器翻译」，并确保该 Access Key 有调用权限。",
};

/** The credentials each service asks for, by keyring name. */
export const SERVICE_CREDENTIALS: Record<string, { name: string; label: string; placeholder: string }[]> = {
  deepl: [{ name: "deepl-key", label: "API Key", placeholder: "免费版以 :fx 结尾" }],
  openai: [{ name: "openai-key", label: "API Key", placeholder: "sk-…" }],
  siliconflow: [{ name: "siliconflow-key", label: "API Key", placeholder: "硅基流动控制台 API Key" }],
  zhipu: [{ name: "zhipu-key", label: "API Key", placeholder: "智谱开放平台 API Key" }],
  baidu: [
    { name: "baidu-appid", label: "APP ID", placeholder: "控制台 APP ID" },
    { name: "baidu-key", label: "密钥", placeholder: "控制台密钥" },
  ],
  volcengine: [
    { name: "volcengine-access-key", label: "Access Key ID", placeholder: "AKLT…" },
    { name: "volcengine-secret-key", label: "Secret Access Key", placeholder: "控制台生成的密钥" },
  ],
  tencent: [
    { name: "tencent-secretid", label: "SecretId", placeholder: "AKID…" },
    { name: "tencent-secretkey", label: "SecretKey", placeholder: "控制台密钥" },
  ],
  youdao: [
    { name: "youdao-appkey", label: "应用 ID", placeholder: "控制台 appKey" },
    { name: "youdao-secret", label: "应用密钥", placeholder: "控制台密钥" },
  ],
  azure: [
    { name: "azure-key", label: "订阅密钥", placeholder: "Key 1 或 Key 2" },
    { name: "azure-region", label: "区域", placeholder: "如 eastasia" },
  ],
  google: [],
  "index-translate": [],
};
