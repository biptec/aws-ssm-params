package textio

// FieldMappings is an ordered set of AWS-to-file field mappings shared by structured codecs.
type FieldMappings []FieldMapping

// objects projects records into mapped file objects.
func (mappings FieldMappings) objects(records Records, keyField string) []map[string]any {
	objects := make([]map[string]any, 0, len(records))
	for i := range records {
		objects = append(objects, mappings.object(records[i], keyField))
	}
	return objects
}

// object projects one record and omits the field represented by an outer object key.
func (mappings FieldMappings) object(record Record, keyField string) map[string]any {
	object := map[string]any{}
	for _, mapping := range mappings {
		if mapping.AWSName == keyField {
			continue
		}
		value := record.fieldAny(mapping.AWSName)
		if value == nil || value == "" {
			if mapping.AWSName != FieldValue || !record.includesField(FieldValue) {
				continue
			}
		}
		object[mapping.FileName] = value
	}
	return object
}

// DefaultFieldMappings returns the default AWS-to-file field mappings.
func DefaultFieldMappings() FieldMappings {
	return append(FieldMappings(nil), defaultFieldMappings()...)
}

// WithDefaults overlays the receiver on the default mappings.
func (mappings FieldMappings) WithDefaults() FieldMappings {
	defaults := DefaultFieldMappings()
	if len(mappings) == 0 {
		return defaults
	}
	overrides := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		overrides[mapping.AWSName] = mapping.FileName
	}
	for i := range defaults {
		if fileName, ok := overrides[defaults[i].AWSName]; ok {
			defaults[i].FileName = fileName
		}
	}
	return defaults
}

// Find returns the mapping for one AWS field.
func (mappings FieldMappings) Find(awsName string) (FieldMapping, bool) {
	for _, mapping := range mappings {
		if mapping.AWSName == awsName {
			return mapping, true
		}
	}
	return FieldMapping{}, false
}

// ForFields returns mappings in field order.
func (mappings FieldMappings) ForFields(fields Fields) FieldMappings {
	out := make(FieldMappings, 0, len(fields))
	for _, field := range fields {
		if mapping, ok := mappings.Find(field); ok {
			out = append(out, mapping)
		}
	}
	return out
}

func defaultFieldMappings() FieldMappings {
	return FieldMappings{
		{AWSName: FieldName, FileName: FieldName},
		{AWSName: FieldRegion, FileName: FieldRegion},
		{AWSName: FieldType, FileName: FieldType},
		{AWSName: FieldTier, FileName: FieldTier},
		{AWSName: FieldDataType, FileName: "dataType"},
		{AWSName: FieldPolicies, FileName: FieldPolicies},
		{AWSName: FieldDescription, FileName: FieldDescription},
		{AWSName: FieldValue, FileName: FieldValue},
		{AWSName: FieldDate, FileName: FieldDate},
		{AWSName: FieldVersion, FileName: FieldVersion},
		{AWSName: FieldLen, FileName: FieldLen},
		{AWSName: FieldSHA256, FileName: FieldSHA256},
		{AWSName: FieldUser, FileName: FieldUser},
	}
}
