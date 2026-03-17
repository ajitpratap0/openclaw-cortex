"use client";

import { useState, useEffect } from "react";
import { cortex, saveSettings, loadSettings } from "@/lib/api";
import { Button }   from "@/components/ui/button";
import { Input }    from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge }    from "@/components/ui/badge";

type HealthStatus = "idle" | "checking" | "ok" | "error";

export default function SettingsPage() {
  const [apiURL,    setApiURL]    = useState("");
  const [apiToken,  setApiToken]  = useState("");
  const [status,    setStatus]    = useState<HealthStatus>("idle");
  const [statusMsg, setStatusMsg] = useState("");
  const [saved,     setSaved]     = useState(false);

  useEffect(() => {
    const { apiURL: url, apiToken: token } = loadSettings();
    setApiURL(url);
    setApiToken(token);
  }, []);

  function handleSave() {
    saveSettings(apiURL, apiToken);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  }

  async function handlePing() {
    setStatus("checking");
    setStatusMsg("");
    try {
      const resp = await cortex.health();
      setStatus("ok");
      setStatusMsg(resp.status);
    } catch (e) {
      setStatus("error");
      setStatusMsg(e instanceof Error ? e.message : String(e));
    }
  }

  const badgeVariant: Record<
    HealthStatus,
    "default" | "secondary" | "destructive" | "outline"
  > = {
    idle:     "outline",
    checking: "secondary",
    ok:       "default",
    error:    "destructive",
  };

  return (
    <div className="max-w-xl space-y-6">
      <h1 className="text-xl font-semibold text-zinc-100">Settings</h1>

      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader>
          <CardTitle className="text-sm text-zinc-300">
            Cortex API Connection
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1">
            <label className="text-xs text-zinc-400">API URL</label>
            <Input
              value={apiURL}
              onChange={(e) => setApiURL(e.target.value)}
              placeholder="http://localhost:8080"
              className="font-mono text-sm"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-zinc-400">
              Bearer Token{" "}
              <span className="text-zinc-600">(leave empty if auth disabled)</span>
            </label>
            <Input
              type="password"
              value={apiToken}
              onChange={(e) => setApiToken(e.target.value)}
              placeholder="sk-..."
              className="font-mono text-sm"
            />
          </div>

          <div className="flex items-center gap-3 pt-2">
            <Button onClick={handleSave} variant="default" size="sm">
              {saved ? "Saved" : "Save"}
            </Button>
            <Button onClick={handlePing} variant="outline" size="sm">
              {status === "checking" ? "Pinging..." : "Test Connection"}
            </Button>
            {status !== "idle" && (
              <Badge variant={badgeVariant[status]}>
                {status === "ok"
                  ? `ok — ${statusMsg}`
                  : status === "checking"
                  ? "..."
                  : `error: ${statusMsg}`}
              </Badge>
            )}
          </div>
        </CardContent>
      </Card>

      <p className="text-xs text-zinc-600">
        Settings are persisted in localStorage. NEXT_PUBLIC_CORTEX_API_URL and
        NEXT_PUBLIC_CORTEX_API_TOKEN are used as defaults when no localStorage
        value is present.
      </p>
    </div>
  );
}
