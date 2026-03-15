import { createFileRoute } from '@tanstack/react-router'
import { Button } from '@pivox/primitives/button'
import { Badge } from '@pivox/primitives/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from '@pivox/primitives/card'

export const Route = createFileRoute('/')({
  component: HomePage
})

function HomePage() {
  const ipcHandle = (): void => window.electron.ipcRenderer.send('ping')

  return (
    <div className="flex min-h-screen items-center justify-center p-8">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle className="text-2xl">Pivox</CardTitle>
            <Badge variant="secondary">Electron</Badge>
          </div>
          <CardDescription>Desktop Application</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            Shared UI components working across Next.js and Electron.
          </p>
          <div className="flex gap-2">
            <Button onClick={ipcHandle}>Send IPC</Button>
            <Button variant="outline" asChild>
              <a href="https://electron-vite.org/" target="_blank" rel="noreferrer">
                Documentation
              </a>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
