import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, test } from "vitest";
import { ShareLimitInput } from "./share-limit-input";

function Harness() {
  const [value, setValue] = useState(1);
  return (
    <>
      <ShareLimitInput kind="ratio" value={value} onChange={setValue} />
      <output>{value}</output>
    </>
  );
}

test("a decimal comma stays as typed while the value updates", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  const input = screen.getByLabelText("Ratio") as HTMLInputElement;
  await user.clear(input);
  await user.type(input, "1,5");
  expect(input.value).toBe("1,5");
  expect(screen.getByRole("status").textContent).toBe("1.5");
});
