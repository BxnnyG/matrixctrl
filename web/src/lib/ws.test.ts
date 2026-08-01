import { describe, it, expect } from "vitest";
import { createLogGate } from "./ws";

describe("createLogGate", () => {
  it("delivers every line on a first connection", () => {
    const gate = createLogGate();
    gate.reset();
    expect([gate.accept(), gate.accept(), gate.accept()]).toEqual([
      true,
      true,
      true,
    ]);
    expect(gate.deliveredCount).toBe(3);
  });

  // The regression this exists for: the server replays the whole log buffer on
  // every connect. Without the gate, one reconnect showed the operator every
  // line twice.
  it("skips the replayed prefix after a reconnect", () => {
    const gate = createLogGate();

    gate.reset();
    gate.accept(); // line 1
    gate.accept(); // line 2

    // Socket drops, client reconnects, server replays lines 1 and 2 then sends 3.
    gate.reset();
    expect(gate.accept()).toBe(false); // replayed line 1
    expect(gate.accept()).toBe(false); // replayed line 2
    expect(gate.accept()).toBe(true); // genuinely new line 3
    expect(gate.deliveredCount).toBe(3);
  });

  it("survives several reconnects in a row", () => {
    const gate = createLogGate();

    gate.reset();
    gate.accept();

    for (let i = 0; i < 3; i++) {
      gate.reset();
      expect(gate.accept()).toBe(false); // the one line we already showed
    }

    expect(gate.deliveredCount).toBe(1);

    gate.reset();
    gate.accept(); // replay
    expect(gate.accept()).toBe(true); // new
    expect(gate.deliveredCount).toBe(2);
  });

  it("handles a reconnect where nothing new arrived", () => {
    const gate = createLogGate();
    gate.reset();
    gate.accept();
    gate.accept();

    gate.reset();
    expect(gate.accept()).toBe(false);
    expect(gate.accept()).toBe(false);
    expect(gate.deliveredCount).toBe(2);
  });
});
