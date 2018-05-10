// Copyright 2015 xeipuuv ( https://github.com/xeipuuv )
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// author           xeipuuv
// author-github    https://github.com/xeipuuv
// author-mail      xeipuuv@gmail.com
//
// repository-name  gojsonschema
// repository-desc  An implementation of JSON Schema, based on IETF's draft v4 - Go language.
//
// description		Defines resources pooling.
//                  Eases referencing and avoids downloading the same resource twice.
//
// created          26-02-2013

package gojsonschema

import (
	"errors"
	"reflect"

	"github.com/xeipuuv/gojsonreference"
)

type schemaPoolDocument struct {
	Document interface{}
}

type schemaPool struct {
	schemaPoolDocuments map[string]*schemaPoolDocument
	standaloneDocument  interface{}
	jsonLoaderFactory   JSONLoaderFactory
}

func newSchemaPool(f JSONLoaderFactory) *schemaPool {

	p := &schemaPool{}
	p.schemaPoolDocuments = make(map[string]*schemaPoolDocument)
	p.standaloneDocument = nil
	p.jsonLoaderFactory = f

	return p
}

func (p *schemaPool) ParseDocument(document interface{}, ref gojsonreference.JsonReference) {
	// ParseDocument parses a JSON document and resolves all $id and $ref references.
	// For $ref references it takes into account the $id scope it is in and replaces
	// the reference by the absolute resolved reference

	// When encountering errors it fails silently. Error handling is done when the schema
	// is syntactically parsed and any error encountered here should also come up there.
	switch m := document.(type) {
	case []interface{}:
		for _, v := range m {
			p.ParseDocument(v, ref)
		}
	case map[string]interface{}:
		localRef := &ref

		keyID := KEY_ID_NEW
		if existsMapKey(m, KEY_ID) {
			keyID = KEY_ID
		}
		if existsMapKey(m, keyID) && isKind(m[keyID], reflect.String) {
			jsonReference, err := gojsonreference.NewJsonReference(m[keyID].(string))
			if err == nil {
				localRef, err = ref.Inherits(jsonReference)
				if err == nil {
					p.schemaPoolDocuments[localRef.String()] = &schemaPoolDocument{Document: document}
				}
			}
		}

		if existsMapKey(m, KEY_REF) && isKind(m[KEY_REF], reflect.String) {
			jsonReference, err := gojsonreference.NewJsonReference(m[KEY_REF].(string))
			if err == nil {
				absoluteRef, err := localRef.Inherits(jsonReference)
				if err == nil {
					m[KEY_REF] = absoluteRef.String()
				}
			}
		}

		for k, v := range m {
			// const and enums should be interpreted literally, so ignore them
			if k == KEY_CONST || k == KEY_ENUM {
				continue
			}
			// Something like a property or a dependency is not a valid schema, as it might describe properties named "$ref", "$id" or "const", etc
			// Therefore don't treat it like a schema.
			if k == KEY_PROPERTIES || k == KEY_DEPENDENCIES || k == KEY_PATTERN_PROPERTIES {
				if child, ok := v.(map[string]interface{}); ok {
					for _, v := range child {
						p.ParseDocument(v, *localRef)
					}
				}
			} else {
				p.ParseDocument(v, *localRef)
			}
		}
	}
}

func (p *schemaPool) SetStandaloneDocument(document interface{}) {
	p.standaloneDocument = document
}

func (p *schemaPool) GetStandaloneDocument() (document interface{}) {
	return p.standaloneDocument
}

func (p *schemaPool) GetDocument(reference gojsonreference.JsonReference) (*schemaPoolDocument, error) {

	var (
		spd *schemaPoolDocument
		ok  bool
		err error
	)

	if internalLogEnabled {
		internalLog("Get Document ( %s )", reference.String())
	}

	// It is not possible to load anything that is not canonical...
	if !reference.IsCanonical() {
		return nil, errors.New(formatErrorDescription(
			Locale.ReferenceMustBeCanonical(),
			ErrorDetails{"reference": reference.String()},
		))
	}

	// Create a deep copy, so we can remove the fragment part later on without altering the original
	refToUrl, _ := gojsonreference.NewJsonReference(reference.String())

	// First check if the given fragment is a location independent identifier
	// http://json-schema.org/latest/json-schema-core.html#rfc.section.8.2.3

	if spd, ok = p.schemaPoolDocuments[refToUrl.String()]; ok {
		if internalLogEnabled {
			internalLog(" From pool")
		}
		return spd, nil
	}

	// If the given reference is not a location independent identifier,
	// strip the fragment and look for a document with it's base URI

	refToUrl.GetUrl().Fragment = ""

	if cachedSpd, ok := p.schemaPoolDocuments[refToUrl.String()]; ok {

		document, _, err := reference.GetPointer().Get(cachedSpd.Document)

		if err != nil {
			return nil, err
		}

		if internalLogEnabled {
			internalLog(" From pool")
		}

		spd = &schemaPoolDocument{Document: document}
		p.schemaPoolDocuments[reference.String()] = spd

		return spd, nil
	}

	jsonReferenceLoader := p.jsonLoaderFactory.New(reference.String())
	document, err := jsonReferenceLoader.LoadJSON()

	if err != nil {
		return nil, err
	}

	mainSpd := &schemaPoolDocument{Document: document}
	// add the whole document to the pool for potential re-use
	p.schemaPoolDocuments[refToUrl.String()] = mainSpd
	p.ParseDocument(document, refToUrl)

	// resolve a potential fragment and also cache it
	document, _, err = reference.GetPointer().Get(document)
	spd = &schemaPoolDocument{Document: document}
	p.schemaPoolDocuments[reference.String()] = mainSpd

	return spd, nil
}
