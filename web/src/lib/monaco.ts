// Self-hosted Monaco.
//
// @monaco-editor/react otherwise fetches the editor from cdn.jsdelivr.net at
// runtime. MatrixCtrl is a self-hosted admin tool that routinely runs without
// outbound internet, so that request fails and every editor renders blank.
// Bundling it locally is the only correct option here.
//
// We deliberately build a minimal Monaco: the core editor plus the YAML grammar.
// Pulling `monaco-editor/esm/vs/editor/editor.main` would bundle every language
// Monaco ships with and add several MB to the embedded Go binary.
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import "monaco-editor/esm/vs/editor/edcore.main";
import "monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution";
import { loader } from "@monaco-editor/react";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";

declare global {
  interface Window {
    MonacoEnvironment?: { getWorker: (workerId: string, label: string) => Worker };
  }
}

// YAML has no dedicated language server worker in core Monaco (highlighting is
// tokenizer-only), so the generic editor worker covers every case we use.
window.MonacoEnvironment = { getWorker: () => new EditorWorker() };

loader.config({ monaco });

export { monaco };
