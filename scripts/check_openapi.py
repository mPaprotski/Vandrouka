#!/usr/bin/env python3
"""Validate OpenAPI, inline examples, and registered versus planned endpoints."""
from pathlib import Path
import re
import argparse
import json
import yaml
from openapi_spec_validator import validate_spec
from openapi_schema_validator import OAS30Validator, oas30_format_checker

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--route-response', type=Path, help='Validate JSON serialized by Go HTTP models')
    args = parser.parse_args()
    spec = yaml.safe_load((ROOT / 'docs/openapi.yaml').read_text())
    validate_spec(spec)

    def resolve(value):
        if '$ref' in value:
            target = value['$ref']
            if not target.startswith('#/'):
                raise ValueError('External references are not allowed in this contract')
            result = spec
            for key in target[2:].split('/'):
                result = result[key]
            return result
        return value

    def expand(node, seen=()):
        if isinstance(node, list):
            return [expand(value, seen) for value in node]
        if not isinstance(node, dict):
            return node
        if '$ref' in node:
            target = node['$ref']
            if target in seen:
                raise ValueError('Recursive schema needs a reference-aware validator')
            return expand(resolve(node), seen + (target,))
        return {key: expand(value, seen) for key, value in node.items()}

    def validator(schema):
        return OAS30Validator(expand(schema), format_checker=oas30_format_checker)

    operations = set()
    count = 0
    for path, item in spec['paths'].items():
        for method, operation in item.items():
            if method not in {'get', 'post'}:
                continue
            if operation['x-implementation-status'] == 'implemented':
                operations.add((method, path))
            bodies = []
            if 'requestBody' in operation:
                bodies.append(resolve(operation['requestBody']))
            bodies.extend(resolve(response) for response in operation['responses'].values())
            for body in bodies:
                for media in body.get('content', {}).values():
                    check = validator(media['schema'])
                    examples = [e['value'] for e in media.get('examples', {}).values()]
                    if 'example' in media:
                        examples.append(media['example'])
                    for example in examples:
                        check.validate(example)
                        count += 1
    registered = set()
    for file in ['backend/internal/http/catalog.go', 'backend/cmd/api/main.go']:
        code = (ROOT / file).read_text()
        registered.update((method.lower(), path) for method, path in re.findall(r'\br\.(Get|Post)\("([^"]+)"', code))
    if operations != registered:
        raise ValueError(f'Implemented contract and Go routes differ: {operations ^ registered}')
    for name in ['RouteVariant', 'RouteStop', 'RouteTotals', 'RouteExplanation', 'RouteDiff']:
        if name not in spec['components']['schemas']:
            raise ValueError(f'Missing route schema {name}')

    # Boundary examples must retain the distinction between null and zero.
    generate = spec['components']['schemas']['GenerateRouteRequest']
    operation = spec['paths']['/v1/routes/generate']['post']
    sample = operation['requestBody']['content']['application/json']['examples']['normal']['value']
    check = validator(generate)
    for budget in [None, 0, 1500]:
        check.validate(dict(sample, ticket_budget_minor=budget))
    omitted = dict(sample)
    omitted.pop('ticket_budget_minor')
    check.validate(omitted)
    invalid = [dict(sample, ticket_budget_minor=-1), dict(sample, budget_minutes=0),
               dict(sample, categories=['parks', 'parks']), dict(sample, pace='running'),
               dict(sample, start_at='2026-10-24T11:00:00'), dict(sample, access_token='not-accepted')]
    for example in invalid:
        if not list(check.iter_errors(example)):
            raise ValueError(f'Invalid request accepted: {example}')
    response_check = validator(spec['components']['schemas']['GenerateRouteResponse'])
    response = operation['responses']['200']['content']['application/json']['examples']['normal']['value']
    empty_variant = dict(response['variants'][0], stops=[])
    if not list(response_check.iter_errors(dict(response, variants=[empty_variant]))):
        raise ValueError('Generate must not return an empty variant')
    recalc = spec['paths']['/v1/routes/recalculate']['post']['requestBody']['content']['application/json']['examples']['normal']['value']
    recalc_check = validator(spec['components']['schemas']['RecalculateRouteRequest'])
    if not list(recalc_check.iter_errors(dict(recalc, base_place_ids=[1, 1]))):
        raise ValueError('Duplicate base stops accepted')
    change = validator(spec['components']['schemas']['RouteChange'])
    for kind in ['add', 'remove']:
        change.validate({'type': kind, 'place_id': 1})
    if not list(change.iter_errors({'type': 'pin', 'place_id': 1})):
        raise ValueError('Unimplemented pin operation accepted')
    if args.route_response is not None:
        response_check.validate(json.loads(args.route_response.read_text()))
        print('Actual Go route response matches OpenAPI')
    print(f'OpenAPI valid; {count} examples and request boundaries checked; {len(registered)} implemented endpoints match Go')


if __name__ == '__main__':
    main()
