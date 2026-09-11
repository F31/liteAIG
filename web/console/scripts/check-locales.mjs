import { readFileSync, readdirSync } from "node:fs";
const root = new URL("../src/locales/", import.meta.url);
const read = (locale, file) =>
  JSON.parse(readFileSync(new URL(`${locale}/${file}`, root), "utf8"));
const flatten = (value, prefix = "") =>
  Object.entries(value)
    .flatMap(([key, item]) =>
      typeof item === "object" && item !== null
        ? flatten(item, prefix ? `${prefix}.${key}` : key)
        : [prefix ? `${prefix}.${key}` : key],
    )
    .sort();
const enFiles = readdirSync(new URL("en-US/", root))
  .filter((file) => file.endsWith(".json"))
  .sort();
const zhFiles = readdirSync(new URL("zh-CN/", root))
  .filter((file) => file.endsWith(".json"))
  .sort();
if (JSON.stringify(enFiles) !== JSON.stringify(zhFiles))
  throw new Error("Locale namespace mismatch");
for (const file of enFiles) {
  const en = flatten(read("en-US", file));
  const zh = flatten(read("zh-CN", file));
  if (JSON.stringify(en) !== JSON.stringify(zh)) {
    const missingEn = zh.filter((key) => !en.includes(key));
    const missingZh = en.filter((key) => !zh.includes(key));
    throw new Error(
      `${file} key mismatch. en-US missing: ${missingEn.join(", ")}; zh-CN missing: ${missingZh.join(", ")}`,
    );
  }
}
