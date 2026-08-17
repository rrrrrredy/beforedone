import { spawn } from "node:child_process";

for (const name of ["BEFOREDONE_CLI", "BEFOREDONE_TEST_ROOT"]) {
  if (!process.env[name]) {
    throw new Error(`${name} is required for the real Cordis/DSH integration test`);
  }
}

const child = spawn(process.execPath, ["--test", "tests/integration.test.mjs"], {
  cwd: new URL("../", import.meta.url),
  env: process.env,
  stdio: "inherit",
  windowsHide: true,
});
child.once("error", (error) => {
  throw error;
});
child.once("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exitCode = code ?? 1;
});
