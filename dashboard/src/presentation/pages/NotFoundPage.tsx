import { Button } from '@chakra-ui/react'
import { Home } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { RouteErrorTemplate } from '@/presentation/components/RouteErrorTemplate'

/** Catch-all 404 state that returns authenticated operators to Overview. */
export function NotFoundPage() {
  const navigate = useNavigate()
  return (
    <RouteErrorTemplate
      code="404"
      title="Page not found"
      message="Check the URL or return to Overview."
      action={
        <Button colorPalette="brand" onClick={() => navigate('/')}>
          <Home size={16} />
          Open Overview
        </Button>
      }
    />
  )
}
