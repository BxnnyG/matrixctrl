import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { ArchivePicker } from "./ArchivePicker";

afterEach(cleanup);

const file = (name: string, bytes: number) =>
  new File([new Uint8Array(bytes)], name, { type: "application/gzip" });

describe("ArchivePicker", () => {
  // The bare input accepted a drop only onto itself, so dropping a file on the page
  // did nothing at all — silently, which is the worst kind of nothing.
  it("takes a file that is dropped on it", () => {
    const onPick = vi.fn();
    render(<ArchivePicker file={null} onPick={onPick} />);

    const zone = screen.getByRole("button");
    const dropped = file("matrixctrl-full.tar.gz", 8);
    fireEvent.drop(zone, { dataTransfer: { files: [dropped] } });

    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick.mock.calls[0][0].name).toBe("matrixctrl-full.tar.gz");
  });

  // "dann ist es da aber dann nichts mehr": between choosing a file and the answer
  // there was no sign that anything was happening, for minutes.
  it("shows how much of the upload has gone out", () => {
    render(<ArchivePicker file={file("a.tar.gz", 4 * 1024 * 1024)} busy sent={1024 * 1024} onPick={vi.fn()} />);
    expect(screen.getByText(/Wird hochgeladen/)).toBeTruthy();
    expect(screen.getByText(/1\.0 \/ 4\.0 MB/)).toBeTruthy();
  });

  it("says what it wants when nothing is chosen yet", () => {
    render(<ArchivePicker file={null} onPick={vi.fn()} />);
    expect(screen.getByText(/ablegen oder klicken/)).toBeTruthy();
  });

  it("shows the reason it refused", () => {
    render(<ArchivePicker file={null} error="Archiv unlesbar" onPick={vi.fn()} />);
    expect(screen.getByText("Archiv unlesbar")).toBeTruthy();
  });
});
