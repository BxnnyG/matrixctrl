interface DiffLine {
  type: "add" | "remove" | "context";
  content: string;
  oldNum?: number;
  newNum?: number;
}

interface DiffHunk {
  header: string;
  lines: DiffLine[];
}

interface DiffFile {
  fromFile: string;
  toFile: string;
  displayName: string;
  hunks: DiffHunk[];
}

export function parseDiff(raw: string): DiffFile[] {
  const files: DiffFile[] = [];
  let cur: DiffFile | null = null;
  let curHunk: DiffHunk | null = null;
  let oldLine = 0;
  let newLine = 0;

  for (const line of raw.split("\n")) {
    if (line.startsWith("--- ")) {
      if (cur) files.push(cur);
      const name = line.slice(4).replace(/^[ab]\//, "");
      cur = { fromFile: line.slice(4), toFile: "", displayName: name, hunks: [] };
      curHunk = null;
    } else if (line.startsWith("+++ ") && cur) {
      cur.toFile = line.slice(4);
      const name = cur.toFile.replace(/^[ab]\//, "");
      if (name !== "/dev/null") cur.displayName = name;
    } else if (line.startsWith("@@ ") && cur) {
      const m = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      oldLine = m ? parseInt(m[1]) : 1;
      newLine = m ? parseInt(m[2]) : 1;
      curHunk = { header: line, lines: [] };
      cur.hunks.push(curHunk);
    } else if (curHunk) {
      if (line.startsWith("+")) {
        curHunk.lines.push({ type: "add", content: line.slice(1), newNum: newLine++ });
      } else if (line.startsWith("-")) {
        curHunk.lines.push({ type: "remove", content: line.slice(1), oldNum: oldLine++ });
      } else if (line.startsWith(" ") || line === "") {
        curHunk.lines.push({ type: "context", content: line.slice(1), oldNum: oldLine++, newNum: newLine++ });
      }
    }
  }
  if (cur) files.push(cur);
  return files;
}

interface DiffViewProps {
  raw: string;
  maxHeight?: string;
}

export function DiffView({ raw, maxHeight = "max-h-[32rem]" }: DiffViewProps) {
  if (!raw || raw.startsWith("(")) {
    return (
      <p className="px-4 py-3 text-xs text-gray-400 dark:text-gray-500 italic">
        {raw || "Kein Diff verfügbar."}
      </p>
    );
  }

  const files = parseDiff(raw);
  if (files.length === 0) {
    return <p className="px-4 py-3 text-xs text-gray-400 dark:text-gray-500 italic">Kein Diff verfügbar.</p>;
  }

  return (
    <div className={`${maxHeight} overflow-y-auto text-xs font-mono`}>
      {/* File nav */}
      {files.length > 1 && (
        <div className="flex flex-wrap gap-1 px-3 py-2 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">
          {files.map((f) => (
            <a
              key={f.displayName}
              href={`#diff-${encodeURIComponent(f.displayName)}`}
              className="px-2 py-0.5 bg-white dark:bg-gray-700 rounded text-blue-600 dark:text-blue-400 hover:underline border border-gray-200 dark:border-gray-600"
            >
              {f.displayName}
            </a>
          ))}
        </div>
      )}

      {files.map((file) => (
        <div key={file.displayName} id={`diff-${encodeURIComponent(file.displayName)}`}>
          {/* File header */}
          <div className="sticky top-0 px-3 py-1.5 bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 font-semibold border-b border-gray-300 dark:border-gray-600 z-10">
            {file.displayName}
          </div>

          {file.hunks.map((hunk, hi) => (
            <div key={hi}>
              {/* Hunk header */}
              <div className="px-3 py-0.5 bg-blue-50 dark:bg-blue-950/40 text-blue-600 dark:text-blue-400 border-y border-blue-100 dark:border-blue-900">
                {hunk.header}
              </div>

              {/* Lines */}
              <table className="w-full border-collapse">
                <tbody>
                  {hunk.lines.map((line, li) => (
                    <tr
                      key={li}
                      className={
                        line.type === "add"
                          ? "bg-green-50 dark:bg-green-950/30"
                          : line.type === "remove"
                          ? "bg-red-50 dark:bg-red-950/30"
                          : "bg-white dark:bg-gray-900"
                      }
                    >
                      {/* Old line number */}
                      <td className="w-10 px-2 py-0 text-right text-gray-400 dark:text-gray-600 select-none border-r border-gray-100 dark:border-gray-800 leading-5">
                        {line.type !== "add" ? line.oldNum : ""}
                      </td>
                      {/* New line number */}
                      <td className="w-10 px-2 py-0 text-right text-gray-400 dark:text-gray-600 select-none border-r border-gray-100 dark:border-gray-800 leading-5">
                        {line.type !== "remove" ? line.newNum : ""}
                      </td>
                      {/* Sign */}
                      <td className={`w-5 px-1 py-0 text-center select-none leading-5 ${
                        line.type === "add" ? "text-green-600 dark:text-green-400" :
                        line.type === "remove" ? "text-red-600 dark:text-red-400" :
                        "text-gray-400"
                      }`}>
                        {line.type === "add" ? "+" : line.type === "remove" ? "−" : ""}
                      </td>
                      {/* Content */}
                      <td className={`py-0 pr-4 leading-5 whitespace-pre ${
                        line.type === "add" ? "text-green-800 dark:text-green-200" :
                        line.type === "remove" ? "text-red-800 dark:text-red-200" :
                        "text-gray-700 dark:text-gray-400"
                      }`}>
                        {line.content || " "}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
