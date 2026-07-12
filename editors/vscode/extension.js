// Galaksija Debug: the whole extension is this file — it only tells
// VS Code how to start the vremeplov-dap adapter; everything else is
// the Debug Adapter Protocol.
//
// Adapter resolution order: the user's galaksija.dapPath setting, the
// binary bundled inside this (platform-specific) extension, then
// vremeplov-dap on PATH.
const vscode = require("vscode");
const path = require("path");
const fs = require("fs");

function bundledBinary(ctx) {
  const exe = process.platform === "win32" ? "vremeplov-dap.exe" : "vremeplov-dap";
  const p = path.join(ctx.extensionPath, "bin", exe);
  return fs.existsSync(p) ? p : null;
}

exports.activate = (ctx) => {
  ctx.subscriptions.push(
    vscode.debug.registerDebugAdapterDescriptorFactory("galaksija", {
      createDebugAdapterDescriptor() {
        const configured = vscode.workspace
          .getConfiguration("galaksija")
          .get("dapPath");
        if (configured) {
          if (!fs.existsSync(configured)) {
            vscode.window.showErrorMessage(
              `galaksija.dapPath points at ${configured}, which does not exist.`
            );
            return undefined;
          }
          return new vscode.DebugAdapterExecutable(configured, []);
        }
        const bundled = bundledBinary(ctx);
        if (bundled) {
          return new vscode.DebugAdapterExecutable(bundled, []);
        }
        // Fall back to PATH (source-installed extension); if that
        // fails VS Code shows its own error, so add a hint up front.
        vscode.window.showWarningMessage(
          "No bundled vremeplov-dap in this build — falling back to PATH. " +
            "Install a release binary or set galaksija.dapPath."
        );
        return new vscode.DebugAdapterExecutable("vremeplov-dap", []);
      },
    })
  );
};

exports.deactivate = () => {};
