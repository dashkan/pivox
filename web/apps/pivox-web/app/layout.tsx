import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Pivox',
  description: 'Pivox — Observability Platform',
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" dir="rtl">
      <body className="min-h-screen bg-background font-sans text-foreground antialiased">
        {children}
      </body>
    </html>
  );
}
