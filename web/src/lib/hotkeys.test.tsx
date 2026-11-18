import { fireEvent, render } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { bindingName, useHotkeys } from "./hotkeys";

function Harness({ onKey, dialog = false }: { onKey: (name: string) => void; dialog?: boolean }) {
  useHotkeys({
    a: () => onKey("a"),
    Enter: () => onKey("Enter"),
    "mod+a": () => onKey("mod+a"),
    "?": () => onKey("?"),
  });
  return (
    <>
      <input aria-label="field" />
      <button type="button">button</button>
      {dialog && <div role="dialog" />}
    </>
  );
}

test("bindingName normalises keys", () => {
  expect(bindingName(new KeyboardEvent("keydown", { key: "A" }))).toBe("a");
  expect(bindingName(new KeyboardEvent("keydown", { key: " " }))).toBe("Space");
  expect(bindingName(new KeyboardEvent("keydown", { key: "a", metaKey: true }))).toBe("mod+a");
  expect(bindingName(new KeyboardEvent("keydown", { key: "a", ctrlKey: true }))).toBe("mod+a");
  expect(bindingName(new KeyboardEvent("keydown", { key: "?", shiftKey: true }))).toBe("?");
});

test("shortcuts fire on the page but not in fields, on buttons or under dialogs", () => {
  const onKey = vi.fn<(name: string) => void>();
  const { getByLabelText, getByRole, rerender } = render(<Harness onKey={onKey} />);
  fireEvent.keyDown(document.body, { key: "a" });
  fireEvent.keyDown(document.body, { key: "a", ctrlKey: true });
  fireEvent.keyDown(document.body, { key: "?" });
  expect(onKey.mock.calls.map((c) => c[0])).toEqual(["a", "mod+a", "?"]);

  onKey.mockClear();
  fireEvent.keyDown(getByLabelText("field"), { key: "a" });
  // Enter on a button presses the button; letters still work there.
  fireEvent.keyDown(getByRole("button"), { key: "Enter" });
  fireEvent.keyDown(getByRole("button"), { key: "a" });
  expect(onKey.mock.calls.map((c) => c[0])).toEqual(["a"]);

  onKey.mockClear();
  rerender(<Harness onKey={onKey} dialog />);
  fireEvent.keyDown(document.body, { key: "a" });
  expect(onKey).not.toHaveBeenCalled();
});
