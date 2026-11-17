import { Link, type ErrorComponentProps } from "@tanstack/react-router";
import { TriangleAlertIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { errorMessage } from "@/lib/api";

export function NotFound() {
  return (
    <Empty className="h-full">
      <EmptyHeader>
        <EmptyTitle>Page not found</EmptyTitle>
        <EmptyDescription>There's nothing at this address.</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" render={<Link to="/" />} nativeButton={false}>
          Back to torrents
        </Button>
      </EmptyContent>
    </Empty>
  );
}

export function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <Empty className="h-full">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <TriangleAlertIcon />
        </EmptyMedia>
        <EmptyTitle>Something went wrong</EmptyTitle>
        <EmptyDescription>{errorMessage(error)}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" onClick={reset}>
          Try again
        </Button>
      </EmptyContent>
    </Empty>
  );
}
