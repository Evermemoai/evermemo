import type { Metadata } from "next";
import { Providers } from "./providers";
import { BottomNav } from "@/components/bottom-nav";
import "./globals.css";

export const metadata: Metadata = {
  title: "Evermemo - Universal memory engine for humans and AI agents",
  description: "Memory dashboard for humans and AI agents",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark" data-theme="dark">
      <body className="min-h-screen bg-background text-foreground antialiased">
        <Providers>
          <main className="mx-auto min-h-screen w-full max-w-6xl px-6 pb-32 pt-8 lg:px-8">
            {children}
          </main>
          <BottomNav />
        </Providers>
      </body>
    </html>
  );
}
