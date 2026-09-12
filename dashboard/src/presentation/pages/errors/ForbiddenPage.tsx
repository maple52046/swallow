import { Button } from '@chakra-ui/react'
import { ArrowLeft } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { RouteErrorTemplate } from '@/presentation/components/RouteErrorTemplate'

/** Shared 403 route shown when authorization blocks an operator surface. */
export function ForbiddenPage() {
  const navigate = useNavigate()
  return (
    <RouteErrorTemplate
      code="403"
      title="Access denied"
      message="Ask an administrator for access."
      action={
        <Button colorPalette="brand" onClick={() => navigate('/')}>
          <ArrowLeft size={16} />
          Open Overview
        </Button>
      }
    />
  )
}
