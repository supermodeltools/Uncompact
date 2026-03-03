#!/usr/bin/env node

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");
const os = require("os");

const BINARY_NAME = os.platform() === "win32" ? "uncompact.exe" : "uncompact";

function findBinary() {
  const localBin = path.join(__dirname, "bin", BINARY_NAME);
  if (fs.existsSync(localBin)) {
    return localBin;
  }

  const globalPaths = [
    path.join(os.homedir(), "go", "bin", BINARY_NAME),
    path.join(os.homedir(), ".local", "bin", BINARY_NAME),
    "/usr/local/bin/" + BINARY_NAME,
    "/opt/homebrew/bin/" + BINARY_NAME,
  ];

  for (const p of globalPaths) {
    if (fs.existsSync(p)) {
      return p;
    }
  }

  return null;
}

function main() {
  const binary = findBinary();

  if (!binary) {
    console.error("[uncompact] Binary not found. Try reinstalling:");
    console.error("  npm install -g uncompact");
    console.error("Or install via Go:");
    console.error("  go install github.com/supermodeltools/uncompact@latest");
    process.exit(1);
  }

  const args = process.argv.slice(2);
  const child = spawn(binary, args, {
    stdio: "inherit",
    env: process.env,
  });

  child.on("error", (err) => {
    console.error(`[uncompact] Failed to run binary: ${err.message}`);
    process.exit(1);
  });

  child.on("close", (code, signal) => {
    if (code === null && signal) {
      process.kill(process.pid, signal);
    } else {
      process.exit(code ?? 0);
    }
  });
}

main();
