"""Build a reference-complete Minsk extract; requires osmium==4.3.1."""
import argparse
import osmium

class Minsk(osmium.SimpleHandler):
    def __init__(self, writer):
        super().__init__()
        self.writer = writer
        self.ids = osmium.IdTracker()

    def node(self, node):
        if node.location.valid() and 53.75 <= node.location.lat <= 54.05 and 27.3 <= node.location.lon <= 27.85:
            self.writer.add_node(node)
            self.ids.add_node(node.id)

    def way(self, way):
        if self.ids.contains_any_references(way):
            self.writer.add_way(way)
            self.ids.add_way(way.id)

    def relation(self, relation):
        # Transit routes and country boundaries are unnecessary for pedestrian MVP.
        if relation.tags.get('type') in ('restriction', 'multipolygon') and self.ids.contains_any_references(relation):
            self.writer.add_relation(relation)

parser = argparse.ArgumentParser()
parser.add_argument('source')
parser.add_argument('output')
args = parser.parse_args()
with osmium.BackReferenceWriter(args.output, ref_src=args.source, overwrite=True) as writer:
    Minsk(writer).apply_file(args.source)
print('Minsk extract written:', args.output)
