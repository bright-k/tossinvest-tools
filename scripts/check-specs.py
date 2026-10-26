#!/usr/bin/env python3
"""Offline checks of immutable snapshots, coverage inventory, refs and docs links."""
import hashlib,json,pathlib,re
ROOT=pathlib.Path(__file__).resolve().parent.parent
manifest=json.loads((ROOT/'specs/tossinvest/manifest.json').read_text())
source={}
for entry in manifest['sources']:
    b=(ROOT/'specs/tossinvest'/entry['file']).read_bytes()
    assert len(b)==entry['bytes'] and hashlib.sha256(b).hexdigest()==entry['sha256'],entry['file']
    doc=json.loads(b);source[entry['file']]=doc
    assert doc['info']['version']==entry['api_version']
    assert doc.get('openapi',doc.get('asyncapi'))==entry['specification_version']
    def refs(v):
        if isinstance(v,dict):
            if '$ref' in v:
                ref=v['$ref'];assert ref.startswith('#/'),ref
                target=doc
                for key in ref[2:].split('/'):target=target[key.replace('~1','/').replace('~0','~')]
            for x in v.values():refs(x)
        elif isinstance(v,list):
            for x in v:refs(x)
    refs(doc)
s=source['openapi.json'];a=source['asyncapi.json'];index=json.loads((ROOT/'docs/api/operations.json').read_text())
actual={(method.upper(),path,op['operationId']) for path,item in s['paths'].items() for method,op in item.items() if method in ('get','post','put','patch','delete')}
listed={(op['method'],op['path'],op['operation_id']) for op in index['rest_operations']}
assert actual==listed and len(actual)==len(index['rest_operations'])==41
assert len(s['paths'])==38
ws=a['operations'];assert len(ws)==10 and len(a['channels'])==4
assert set(ws)=={op['operation_id'] for op in index['websocket_operations']}
for op in index['websocket_operations']:
    raw=ws[op['operation_id']]
    assert raw['action']==op['action'] if 'action' in op else raw['action']==op['direction']
reads=[op for op in index['rest_operations'] if op['exposure']=='read_tool'];assert len(reads)==34
names=[op['mcp_tool'] for op in reads]+[op['mcp_tool'] for op in index['additional_tools'] if op['first_milestone'] in ('R1','R2')]
assert len(names)==len(set(names))==39
for group in ('rest_operations','websocket_operations','additional_tools'):
    for op in index[group]:
        if op['status'] in ('verified','live-verified'):
            assert op['verification_evidence']
            for path in op['verification_evidence']:assert (ROOT/path.split('#')[0]).exists(),path
links=0
for path in [ROOT/'README.md',ROOT/'AGENTS.md',*ROOT.glob('docs/**/*.md'),ROOT/'specs/tossinvest/README.md']:
    text=path.read_text();assert len(re.findall(r'^```',text,re.M))%2==0,path
    assert not re.search(r'^(<<<<<<<|=======|>>>>>>>)',text,re.M),path
    for label,target in re.findall(r'\[([^\]]+)\]\(([^)]+)\)',text):
        if re.match(r'\w+://',target) or target.startswith('#'):continue
        destination=target.split('#')[0]
        assert (path.parent/destination).exists(),(str(path),target)
        links+=1
print(json.dumps({'snapshot_hashes':'passed','rest_operations':41,'readonly_rest':34,'ws_operations':10,'R1_tools':35,'R2_tools':39,'markdown_links':links},ensure_ascii=False))
