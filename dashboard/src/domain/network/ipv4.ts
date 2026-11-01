/** Whether `value` is a dotted-quad IPv4 address. */
export function validIPv4Address(value: string): boolean {
  const parts = value.split('.')
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
}

/** Whether `value` is an IPv4 CIDR block such as 10.244.0.0/16. */
export function validIPv4CIDR(value: string): boolean {
  const [address, prefix, ...rest] = value.split('/')
  const numericPrefix = Number(prefix)
  return rest.length === 0 && validIPv4Address(address) && /^\d{1,2}$/.test(prefix ?? '') && numericPrefix >= 0 && numericPrefix <= 32
}
