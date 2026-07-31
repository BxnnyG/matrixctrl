// monaco-editor's package.json exports map has no `types` entry for subpaths
// (only `"./*": "./*"`), so TypeScript can't resolve the deep ESM imports we use
// in lib/monaco.ts to self-host the editor. The runtime paths are valid — these
// declarations just tell tsc what they are.
declare module "monaco-editor/esm/vs/editor/editor.api" {
  export * from "monaco-editor";
}
declare module "monaco-editor/esm/vs/editor/edcore.main";
declare module "monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution";
