// Build Sotto's offline dictionary from the pinned ECDICT CSV.
// curl -fL https://raw.githubusercontent.com/skywind3000/ECDICT/39cfb9672c0906b63569e0453c2e6abfb9ab182a/ecdict.csv -o /tmp/ecdict.csv
// bun scripts/build_dictionary.ts /tmp/ecdict.csv

import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { gzipSync } from "node:zlib";

const dataDir = new URL("../internal/translate/dictionarydata/", import.meta.url);
const usage = "bun scripts/build_dictionary.ts <ecdict.csv> [--output <ecdict.csv.gz>]";
const frequencyLimit = 30_000;
const han = /[\u3400-\u9fff\uf900-\ufaff\u{20000}-\u{323af}]/u;
const inflections = new Set(["p", "d", "3", "i", "s", "r", "t"]);

function* csvRows(source: string): Generator<string[], void> {
  // Match one field at the current offset, including quoted commas and line breaks.
  const field = /(?:"((?:[^"]|"")*)"|([^",\r\n]*))(,|\r\n?|\n|$)/y;
  let row: string[] = [];
  while (field.lastIndex < source.length) {
    const offset = field.lastIndex;
    const match = field.exec(source);
    if (!match) throw new Error(`Invalid CSV field at offset ${offset}`);
    const [, quoted, unquoted, separator] = match;
    row.push(quoted?.replaceAll('""', '"') ?? unquoted ?? "");
    if (separator !== ",") {
      if (row.length > 1 || row[0] !== "") yield row;
      row = [];
    }
  }
  if (row.length > 0) yield [...row, ""];
}

function selectEntries(source: string) {
  const rows = csvRows(source);
  const header = rows.next().value;
  function column(name: string): number {
    const index = header?.indexOf(name) ?? -1;
    if (index < 0) throw new Error(`Missing ECDICT column: ${name}`);
    return index;
  }

  const wordColumn = column("word");
  const phoneticColumn = column("phonetic");
  const translationColumn = column("translation");
  const exchangeColumn = column("exchange");
  const rankColumns = [column("frq"), column("bnc")];

  const entries: { word: string; phonetic: string; meanings: string; lemma: string }[] = [];
  const frequent = new Set<string>();
  const inflected = new Set<string>();
  for (const row of rows) {
    const word = row[wordColumn] ?? "";
    const phonetic = (row[phoneticColumn] ?? "").trim().replace(/^\/+|\/+$/g, "").replaceAll("'", "ˈ").replaceAll(":", "ː");
    const meanings = (row[translationColumn] ?? "").replaceAll("\\n", "\n").trim();
    if (!/^[A-Za-z]+$/.test(word) || !phonetic || !han.test(meanings)) continue;

    const isFrequent = rankColumns.some((index) => {
      const rank = Number(row[index]);
      return Number.isInteger(rank) && rank > 0 && rank <= frequencyLimit;
    });
    if (isFrequent) frequent.add(word.toLowerCase());

    let lemma = "";
    for (const form of (row[exchangeColumn] ?? "").split("/")) {
      const separator = form.indexOf(":");
      if (separator < 0) continue;
      const key = form.slice(0, separator);
      const value = form.slice(separator + 1).toLowerCase();
      if (key === "0" && !lemma) lemma = value;
      if (isFrequent && inflections.has(key)) inflected.add(value);
    }
    entries.push({ word, phonetic, meanings, lemma });
  }

  const selected = new Map<string, [phonetic: string, meanings: string]>();
  for (const { word, phonetic, meanings, lemma } of entries) {
    const key = word.toLowerCase();
    if (frequent.has(key) || inflected.has(key) || frequent.has(lemma)) {
      // Prefer the lowercase entry when the source contains multiple cases.
      if (word === key || !selected.has(key)) selected.set(key, [phonetic, meanings]);
    }
  }
  return selected;
}

function csvField(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

async function main() {
  const { values, positionals } = parseArgs({
    allowPositionals: true,
    options: { output: { type: "string" }, help: { type: "boolean", short: "h" } },
  });
  if (values.help) {
    console.log(usage);
    return;
  }
  const [source] = positionals;
  if (!source || positionals.length !== 1) throw new Error(usage);

  const entries = selectEntries(await readFile(source, "utf8"));
  if (entries.size === 0) throw new Error("ECDICT source did not contain any usable entries");

  const license = await readFile(new URL("LICENSE", dataDir), "utf8");
  const lines = ["# ECDICT: https://github.com/skywind3000/ECDICT"];
  for (const line of license.replace(/\r?\n$/, "").split(/\r?\n/)) lines.push(`# ${line}`);
  for (const [word, [phonetic, meanings]] of [...entries].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) {
    lines.push([word, phonetic, meanings].map(csvField).join(","));
  }
  const data = gzipSync(lines.join("\n") + "\n", { level: 9 });
  await writeFile(values.output ?? fileURLToPath(new URL("ecdict.csv.gz", dataDir)), data);
  console.log(`Wrote ${entries.size.toLocaleString("en-US")} entries (${data.byteLength.toLocaleString("en-US")} bytes)`);
}

try {
  await main();
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
