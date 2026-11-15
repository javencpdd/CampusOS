import { readFile, writeFile, mkdir, copyFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const root = dirname(fileURLToPath(import.meta.url));
const output = resolve(root, "../dist/user");
await mkdir(output, { recursive: true });
for (const name of [
  "index.html",
  "main.js",
  "model.js",
  "parser.js",
  "bridge.js",
  "style.css",
])
  await copyFile(resolve(root, name), resolve(output, name));
const manifestPath = resolve(root, "../plugin.json");
const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
await writeFile(
  resolve(output, "identity.js"),
  "export const identity = " +
    JSON.stringify({
      plugin_key: manifest.publisher + "." + manifest.key,
      plugin_version: manifest.version,
      surface_id: "ui",
      audience: "user",
    }) +
    ";\n",
);
manifest.ui[0].digest =
  "sha256:" +
  createHash("sha256")
    .update(await readFile(resolve(output, "index.html")))
    .digest("hex");
await writeFile(manifestPath, JSON.stringify(manifest, null, 2) + "\n");
console.log(
  "RSS Publisher UI built; signing and installation are separate steps.",
);
