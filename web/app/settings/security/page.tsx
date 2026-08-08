"use client";

import { useEffect, useState } from "react";
import { Button, Card, Chip, Label, TextField, Input, toast } from "@heroui/react";
import { api, getSettings, saveSettings, type SecurityInfo } from "@/lib/api";

export default function SecurityPage() {
  const [hubUrl, setHubUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [agent, setAgent] = useState("");
  const [info, setInfo] = useState<SecurityInfo | null>(null);

  useEffect(() => {
    const s = getSettings();
    setHubUrl(s.hubUrl);
    setApiKey(s.apiKey);
    setAgent(s.agent);
    api.security().then(setInfo).catch(() => setInfo(null));
  }, []);

  function saveConn() {
    saveSettings({ hubUrl: hubUrl.trim(), apiKey: apiKey.trim(), agent: agent.trim() });
    toast("Connection saved", { variant: "success" });
    api.security().then(setInfo).catch(() => setInfo(null));
  }

  async function testConnection() {
    saveConn();
    try {
      const h = await api.health();
      toast(`Connected — ${h.memories} memories on the hub`, { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Connection failed", { variant: "danger" });
    }
  }

  return (
    <div className="flex max-w-2xl flex-col gap-8">
      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-medium">Hub connection</h2>
          <p className="text-sm text-muted-foreground">
            Your credentials stay in this browser (localStorage), never on a server
          </p>
        </div>
        <Card>
          <Card.Content className="flex flex-col gap-4">
            <TextField value={hubUrl} onChange={setHubUrl}>
              <Label>Hub URL</Label>
              <Input placeholder="http://localhost:7777" variant="secondary" />
            </TextField>
            <TextField type="password" value={apiKey} onChange={setApiKey}>
              <Label>API key</Label>
              <Input placeholder="your agent key" variant="secondary" />
            </TextField>
            <TextField value={agent} onChange={setAgent}>
              <Label>Agent name</Label>
              <Input placeholder="dashboard" variant="secondary" />
            </TextField>
            <div className="flex gap-2">
              <Button onPress={saveConn}>Save</Button>
              <Button variant="secondary" onPress={testConnection}>
                Test connection
              </Button>
            </div>
          </Card.Content>
        </Card>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-medium">Hub security</h2>
          <p className="text-sm text-muted-foreground">
            How this hub authenticates callers. Keys are managed on the server
            (EVERMEMO_AGENT_KEYS or EVERMEMO_KEYS_FILE) and never shown here.
          </p>
        </div>
        <Card>
          <Card.Content className="flex flex-col gap-1">
            {info ? (
              <>
                <Row label="Authentication" value={<Chip size="sm" variant="soft">{info.auth_mode}</Chip>} />
                <Row
                  label="Agent identities"
                  value={
                    info.agents.length ? (
                      <div className="flex max-w-[260px] flex-wrap justify-end gap-1">
                        {info.agents.map((a) => (
                          <Chip key={a} size="sm" variant="soft" color={a === info.caller ? "accent" : "default"}>
                            {a}
                          </Chip>
                        ))}
                      </div>
                    ) : (
                      <span className="text-sm text-muted-foreground">none configured</span>
                    )
                  }
                />
                <Row
                  label="Namespace ACLs"
                  value={<Chip size="sm" variant="soft" color={info.acl_enabled ? "success" : "default"}>{info.acl_enabled ? "enabled" : "off"}</Chip>}
                />
                <Row
                  label="Rate limiting"
                  value={<Chip size="sm" variant="soft" color={info.rate_limited ? "success" : "default"}>{info.rate_limited ? "enabled" : "off"}</Chip>}
                  last
                />
              </>
            ) : (
              <span className="py-2 text-sm text-muted-foreground">
                Connect to a hub to see its security configuration.
              </span>
            )}
          </Card.Content>
        </Card>
      </section>
    </div>
  );
}

function Row({ label, value, last }: { label: string; value: React.ReactNode; last?: boolean }) {
  return (
    <div className={`flex items-center justify-between gap-4 py-3 ${last ? "" : "border-b border-separator"}`}>
      <span className="text-sm">{label}</span>
      {value}
    </div>
  );
}
