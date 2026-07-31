// Lazy boundary around Monaco.
//
// Monaco is ~3.6 MB and only the config screens need it. Importing it (or
// @/lib/monaco) at the top level of a route file does NOT keep it out of the
// entry bundle: TanStack's code splitter moves only the component into a lazy
// chunk and leaves the route file's other top-level imports in the statically
// linked reference file — which made Vite emit a modulepreload for Monaco on
// every page load, including login. Going through React.lazy makes it a genuine
// dynamic import.
import { Suspense, lazy, type ComponentProps } from "react";
import type MonacoEditorType from "@monaco-editor/react";
import { Spinner } from "@/components/mc";

type EditorProps = ComponentProps<typeof MonacoEditorType>;

const LazyEditor = lazy(async () => {
  // Order matters: lib/monaco calls loader.config() with the bundled instance,
  // which must happen before <Editor> mounts or the library falls back to its
  // CDN default.
  await import("@/lib/monaco");
  const mod = await import("@monaco-editor/react");
  return { default: mod.default };
});

function EditorFallback() {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 8, height: "100%", minHeight: 160, fontSize: 13, color: "var(--text-faint)" }}>
      <Spinner size={14} /> Editor wird geladen…
    </div>
  );
}

export function YamlEditor(props: EditorProps) {
  return (
    <Suspense fallback={<EditorFallback />}>
      <LazyEditor {...props} />
    </Suspense>
  );
}
