/**
 * Reads an endpoint's JSON request and response shapes from the generated OpenAPI document
 * (`generated/openapi.ts`, the provider's contract), so an adapter that types its call with them is
 * checked against the contract where it calls the endpoint: returning a contract body as a domain
 * type compiles only when every value the contract allows is a value of that type.
 *
 * Both pick an operation by operationId (`operations['…']`) rather than by path, so a moved route
 * keeps its adapter.
 */

/**
 * The `application/json` body of one success status of a contract operation. The constraint makes
 * a wrong status, or a status whose body is not JSON (a 204, plain text), a compile error.
 */
export type JsonResponse<
  Op extends { responses: Record<Status, { content: { 'application/json': unknown } }> },
  Status extends number,
> = Op['responses'][Status]['content']['application/json']

/** The `application/json` request body of a contract operation that accepts one. */
export type JsonRequestBody<
  Op extends { requestBody?: { content: { 'application/json': unknown } } },
> = NonNullable<Op['requestBody']>['content']['application/json']
