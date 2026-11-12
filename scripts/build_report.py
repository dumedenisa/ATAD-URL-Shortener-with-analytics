#!/usr/bin/env python3
"""Build docs/report.docx from report.md using only the Python standard library."""
from pathlib import Path
from xml.sax.saxutils import escape
from zipfile import ZipFile, ZIP_DEFLATED

ROOT = Path(__file__).resolve().parents[1]
NS = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'


def paragraph(text, style=None):
    props = '<w:pPr><w:pStyle w:val="{}"/></w:pPr>'.format(style) if style else ''
    return '<w:p>{}<w:r><w:t xml:space="preserve">{}</w:t></w:r></w:p>'.format(props, escape(text))


def main():
    body = []
    pending = []
    code = False

    def flush():
        if pending:
            body.append(paragraph(' '.join(pending)))
            pending.clear()

    for line in (ROOT / 'docs/report.md').read_text().splitlines():
        if line.startswith('```'):
            flush()
            code = not code
        elif code:
            body.append(paragraph(line, 'Code'))
        elif line == '---':
            flush()
            body.append('<w:p><w:r><w:br w:type="page"/></w:r></w:p>')
        elif line.startswith('# '):
            flush()
            body.append(paragraph(line[2:], 'Title'))
        elif line.startswith('## '):
            flush()
            body.append(paragraph(line[3:], 'Heading1'))
        elif line.startswith('### '):
            flush()
            body.append(paragraph(line[4:], 'Heading2'))
        elif line.startswith('- '):
            flush()
            body.append(paragraph('• ' + line[2:]))
        elif not line.strip():
            flush()
        else:
            pending.append(line)
    flush()
    assert not code, 'Unclosed code block'
    document = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="{}"><w:body>{}<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:bottom="1134" w:left="1134" w:right="1134"/></w:sectPr></w:body></w:document>'.format(NS, ''.join(body))
    styles = '''<w:styles xmlns:w="%s">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="160"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:pPr><w:keepNext/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:pPr><w:spacing w:after="40"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/><w:sz w:val="18"/></w:rPr></w:style></w:styles>''' % NS
    files = {
        '[Content_Types].xml': '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>',
        '_rels/.rels': '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
        'word/_rels/document.xml.rels': '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>',
        'word/document.xml': document,
        'word/styles.xml': styles,
    }
    with ZipFile(ROOT / 'docs/report.docx', 'w', ZIP_DEFLATED) as output:
        for name, content in files.items():
            output.writestr(name, content.encode('utf-8'))
    print('Generated docs/report.docx')


if __name__ == '__main__':
    main()
