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
      message="Your account does not have permission to open this operator surface."
      action={
        <Button colorPalette="brand" onClick={() => navigate('/')}>
          <ArrowLeft size={16} />
          Back to Overview
        </Button>
      }
    />
  )
}
