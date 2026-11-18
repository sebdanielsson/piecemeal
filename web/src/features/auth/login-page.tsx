import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { KeyRoundIcon, LogInIcon } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, apiPrefix } from "@/api/client";
import { cn } from "cn";
import { Button, buttonVariants } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel, FieldSeparator } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { ApiError, unwrap } from "@/lib/api";
import { authConfigQuery, keys } from "@/lib/queries";
import { Logo } from "../shell/logo";

/** Only same-origin paths, so a crafted link can't send people elsewhere. */
const unsafe = (c: string) => c <= " " || c === "\x7f" || c === "\\";

export function safeRedirect(target: string | undefined): string {
  // Browsers drop tabs and newlines in URLs, so "/\t/evil.example" would
  // become "//evil.example": refuse whitespace, control characters and
  // backslashes anywhere.
  if (!target || !target.startsWith("/") || target.startsWith("//") || [...target].some(unsafe)) {
    return "/";
  }
  return target.startsWith("/login") ? "/" : target;
}

export function LoginPage() {
  const { redirect, error: ssoError } = useSearch({ from: "/login" });
  const config = useQuery(authConfigQuery);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");

  const login = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/login", { body: { username, password } })),
    onSuccess: async (principal) => {
      queryClient.setQueryData(keys.me, principal);
      await navigate({ href: safeRedirect(redirect), replace: true });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  const loginError =
    login.error instanceof ApiError
      ? login.error.status === 401
        ? "Wrong username or password."
        : login.error.status === 429
          ? "Too many attempts. Wait a moment and try again."
          : login.error.message
      : login.error
        ? "Could not reach piecemeal."
        : undefined;

  const localLogin = config.data?.localLogin ?? true;
  const oidc = config.data?.oidc ?? false;
  const oidcName = config.data?.oidcName || "SSO";

  return (
    <main className="flex min-h-full items-center justify-center bg-muted/40 p-4">
      <div className="w-full max-w-sm border bg-background p-8 shadow-sm">
        <div className="mb-8 flex flex-col items-center gap-3 text-center">
          <Logo className="size-10" />
          <h1 className="font-heading text-3xl">piecemeal</h1>
          <p className="text-sm text-muted-foreground">Sign in to manage your torrents.</p>
        </div>
        {config.isPending ? (
          <div className="flex justify-center py-6">
            <Spinner />
          </div>
        ) : (
          <FieldGroup>
            {ssoError && (
              <p role="alert" className="text-center text-sm text-destructive">
                {ssoError}
              </p>
            )}
            {oidc && (
              <a
                href={`${apiPrefix}/auth/oidc/login?next=${encodeURIComponent(safeRedirect(redirect))}`}
                className={cn(
                  buttonVariants({ variant: localLogin ? "outline" : "default" }),
                  "w-full",
                )}
              >
                <KeyRoundIcon data-icon="inline-start" />
                Sign in with {oidcName}
              </a>
            )}
            {oidc && localLogin && <FieldSeparator>or</FieldSeparator>}
            {localLogin && (
              <form onSubmit={submit} className="flex flex-col gap-6" noValidate>
                <Field>
                  <FieldLabel htmlFor="username">Username</FieldLabel>
                  <Input
                    id="username"
                    autoComplete="username"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    required
                  />
                </Field>
                <Field data-invalid={loginError ? true : undefined}>
                  <FieldLabel htmlFor="password">Password</FieldLabel>
                  <Input
                    id="password"
                    type="password"
                    autoComplete="current-password"
                    autoFocus
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-invalid={loginError ? true : undefined}
                    aria-describedby={loginError ? "login-error" : undefined}
                    required
                  />
                  {loginError && <FieldError id="login-error">{loginError}</FieldError>}
                </Field>
                <Button type="submit" className="w-full" disabled={login.isPending || !password}>
                  {login.isPending ? <Spinner /> : <LogInIcon data-icon="inline-start" />}
                  Sign in
                </Button>
              </form>
            )}
            {!localLogin && !oidc && (
              <p className="text-center text-sm text-muted-foreground">
                No sign-in method is enabled. Check piecemeal's configuration.
              </p>
            )}
          </FieldGroup>
        )}
      </div>
    </main>
  );
}
