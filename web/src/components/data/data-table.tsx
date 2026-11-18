import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { cn } from "cn";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export interface DataColumn<T> {
  id: string;
  header: string;
  cell: (row: T) => ReactNode;
  /** A sort key; columns without one can't be sorted. */
  sort?: (row: T) => string | number;
  align?: "right";
  className?: string;
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/** A small sortable table for lists that never get long (peers, keys, sessions). */
export function DataTable<T>({
  rows,
  columns,
  rowKey,
  initialSort,
  empty,
  label,
  rowClassName,
}: {
  rows: readonly T[];
  columns: DataColumn<T>[];
  rowKey: (row: T) => string;
  initialSort?: { id: string; desc: boolean };
  empty?: ReactNode;
  label: string;
  rowClassName?: (row: T) => string | undefined;
}) {
  const [sort, setSort] = useState(initialSort);
  const sorted = useMemo(() => {
    const col = columns.find((c) => c.id === sort?.id);
    if (!col?.sort || !sort) {
      return rows;
    }
    const key = col.sort;
    const dir = sort.desc ? -1 : 1;
    return rows.toSorted((a, b) => {
      const x = key(a);
      const y = key(b);
      const c =
        typeof x === "number" && typeof y === "number"
          ? x - y
          : collator.compare(String(x), String(y));
      return c * dir;
    });
  }, [rows, columns, sort]);

  return (
    <Table aria-label={label}>
      <TableHeader>
        <TableRow>
          {columns.map((c) => {
            const active = sort?.id === c.id;
            return (
              <TableHead
                key={c.id}
                aria-sort={active ? (sort.desc ? "descending" : "ascending") : undefined}
                className={cn("h-8 text-xs", c.align === "right" && "text-right", c.className)}
              >
                {c.sort ? (
                  <button
                    type="button"
                    className={cn(
                      "inline-flex items-center gap-1 uppercase hover:text-foreground focus-visible:text-foreground focus-visible:outline-none",
                      active && "text-foreground",
                    )}
                    onClick={() =>
                      setSort(
                        active
                          ? { id: c.id, desc: !sort.desc }
                          : { id: c.id, desc: c.align === "right" },
                      )
                    }
                  >
                    {c.header}
                    {active &&
                      (sort.desc ? (
                        <ArrowDownIcon className="size-3" />
                      ) : (
                        <ArrowUpIcon className="size-3" />
                      ))}
                  </button>
                ) : (
                  c.header
                )}
              </TableHead>
            );
          })}
        </TableRow>
      </TableHeader>
      <TableBody>
        {sorted.length === 0 ? (
          <TableRow>
            <TableCell colSpan={columns.length} className="py-6 text-center text-muted-foreground">
              {empty ?? "Nothing here."}
            </TableCell>
          </TableRow>
        ) : (
          sorted.map((row) => (
            <TableRow key={rowKey(row)} className={rowClassName?.(row)}>
              {columns.map((c) => (
                <TableCell
                  key={c.id}
                  className={cn("py-1.5", c.align === "right" && "text-right tabular", c.className)}
                >
                  {c.cell(row)}
                </TableCell>
              ))}
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}
