"use client";

import { useEffect, useState } from "react";
import { Button, Card, Chip, Skeleton } from "@heroui/react";
import { api, type BillingInfo } from "@/lib/api";

export default function BillingPage() {
  const [info, setInfo] = useState<BillingInfo | null>(null);

  useEffect(() => {
    api.billing().then(setInfo).catch(() =>
      setInfo({ plan: "open-source", price: "free forever", license: "MIT", limits: {} })
    );
  }, []);

  return (
    <div className="flex max-w-2xl flex-col gap-8">
      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-medium">Plan</h2>
          <p className="text-sm text-muted-foreground">Your current evermemo plan</p>
        </div>
        <Card>
          <Card.Content className="flex flex-col gap-4">
            {info ? (
              <>
                <div className="flex items-center justify-between">
                  <div className="flex flex-col">
                    <span className="text-base font-medium capitalize">{info.plan.replace("-", " ")}</span>
                    <span className="text-sm text-muted-foreground">
                      {info.price} · {info.license} licensed
                    </span>
                  </div>
                  <Chip color="success" variant="soft">
                    active
                  </Chip>
                </div>
                <div className="flex flex-col gap-1 border-t border-separator pt-3">
                  {Object.entries(info.limits).map(([k, v]) => (
                    <div key={k} className="flex items-center justify-between py-1">
                      <span className="text-sm capitalize">{k}</span>
                      <span className="text-sm text-muted-foreground">{v}</span>
                    </div>
                  ))}
                </div>
              </>
            ) : (
              <Skeleton className="h-24 w-full rounded-lg" />
            )}
          </Card.Content>
        </Card>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-medium">Evermemo Cloud</h2>
          <p className="text-sm text-muted-foreground">Coming later</p>
        </div>
        <Card>
          <Card.Content className="flex flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              A hosted hub with TLS, backups, key management, and team access — for teams that
              don&apos;t want to run their own. Self-hosting stays free and fully featured, always.
            </p>
            <div>
              <Button variant="secondary" isDisabled>
                Join the waitlist (soon)
              </Button>
            </div>
          </Card.Content>
        </Card>
      </section>
    </div>
  );
}
