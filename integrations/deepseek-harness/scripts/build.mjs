import { copyFile, mkdir, readdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const integrationRoot = fileURLToPath(new URL("../", import.meta.url));
const repositoryRoot = fileURLToPath(new URL("../../../", import.meta.url));
const sourceRoot = path.join(integrationRoot, "src");
const outputRoot = path.join(integrationRoot, "lib");

await Promise.all([
  rm(outputRoot, { recursive: true, force: true }),
  rm(path.join(integrationRoot, "LICENSE"), { force: true }),
]);
await mkdir(outputRoot, { recursive: true });

const entries = await readdir(sourceRoot, { withFileTypes: true });
for (const entry of entries) {
  if (!entry.isFile() || !entry.name.endsWith(".js")) continue;
  await copyFile(path.join(sourceRoot, entry.name), path.join(outputRoot, entry.name));
}
await copyFile(path.join(repositoryRoot, "LICENSE"), path.join(integrationRoot, "LICENSE"));

console.log(`Built ${entries.filter((entry) => entry.isFile() && entry.name.endsWith(".js")).length} runtime modules.`);
