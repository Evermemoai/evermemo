"use client";

import { useEffect, useState } from "react";
import { Avatar, Button, Card, Label, TextField, Input, toast } from "@heroui/react";
import { api, type Account } from "@/lib/api";

export default function ProfilePage() {
  const [account, setAccount] = useState<Account | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [title, setTitle] = useState("");
  const [username, setUsername] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .account()
      .then((a) => {
        setAccount(a);
        setName(a.name ?? "");
        setEmail(a.email ?? "");
        setTitle(a.title ?? "");
        setUsername(a.username ?? "");
      })
      .catch(() => setAccount({}));
  }, []);

  async function save() {
    setBusy(true);
    try {
      await api.saveAccount({
        ...account,
        name: name.trim(),
        email: email.trim(),
        title: title.trim(),
        username: username.trim(),
      });
      toast("Profile saved", { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Save failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  const initials =
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("") || "?";

  return (
    <div className="flex max-w-2xl flex-col gap-3">
      <div>
        <h2 className="text-lg font-medium">Profile</h2>
        <p className="text-sm text-muted-foreground">
          Stored on your hub, keyed to your agent identity
        </p>
      </div>
      <Card>
        <Card.Content className="flex flex-col gap-4">
          <div className="flex items-center gap-4">
            <Avatar size="lg">
              <Avatar.Fallback>{initials}</Avatar.Fallback>
            </Avatar>
            <div className="flex flex-col">
              <span className="font-medium">{name || "Unnamed"}</span>
              <span className="text-sm text-muted-foreground">{email || "no email set"}</span>
            </div>
          </div>
          <TextField value={name} onChange={setName}>
            <Label>Full name</Label>
            <Input placeholder="Ada Lovelace" variant="secondary" />
          </TextField>
          <TextField type="email" value={email} onChange={setEmail}>
            <Label>Email</Label>
            <Input placeholder="ada@example.com" variant="secondary" />
          </TextField>
          <TextField value={title} onChange={setTitle}>
            <Label>Title</Label>
            <Input placeholder="Founder / ML engineer / …" variant="secondary" />
          </TextField>
          <TextField value={username} onChange={setUsername}>
            <Label>Username</Label>
            <Input placeholder="one word, like a nickname" variant="secondary" />
          </TextField>
          <div>
            <Button isDisabled={busy || !account} onPress={save}>
              Save profile
            </Button>
          </div>
        </Card.Content>
      </Card>
    </div>
  );
}
