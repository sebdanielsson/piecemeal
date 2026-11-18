import { useQuery } from "@tanstack/react-query";
import { categoriesQuery } from "@/lib/queries";
import { OptionSelect } from "./option-select";

// Select items can't use "" as a value; this stands for no category.
const none = "\u0000none";

export function CategorySelect({
  id,
  value,
  onChange,
  disabled,
  className,
}: {
  id?: string;
  value: string;
  onChange: (category: string) => void;
  disabled?: boolean;
  className?: string;
}) {
  const { data: categories = [] } = useQuery(categoriesQuery);
  const options = [
    { value: none, label: "None" },
    ...categories.map((c) => ({ value: c.name, label: c.name })),
  ];
  // A category the list doesn't know yet (just created elsewhere) still shows.
  if (value && !categories.some((c) => c.name === value)) {
    options.push({ value, label: value });
  }
  return (
    <OptionSelect
      id={id}
      value={value || none}
      onChange={(v) => onChange(v === none ? "" : v)}
      options={options}
      disabled={disabled}
      className={className}
    />
  );
}
