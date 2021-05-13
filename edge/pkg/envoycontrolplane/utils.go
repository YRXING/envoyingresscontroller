package envoycontrolplane

//func UnmarshalSecret(data []byte) (*EnvoySecret, error) {
//	var envoySecret map[string]interface{}
//	//type Secret struct {
//	//	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
//	//	// Types that are assignable to Type:
//	//	//	*Secret_TlsCertificate
//	//	//	*Secret_SessionTicketKeys
//	//	//	*Secret_ValidationContext
//	//	//	*Secret_GenericSecret
//	//	Type isSecret_Type `protobuf_oneof:"type"`
//	//}
//	var secret map[string]interface{}
//	var secretType map[string]interface{}
//	err := json.Unmarshal(data, envoySecret)
//	if err != nil {
//		return nil, err
//	}
//	err = json.Unmarshal(envoySecret["secret"].([]byte), secret)
//	if err != nil {
//		return nil, err
//	}
//	switch secret["Type"].(type) {
//	case *envoy_tls_v3.Secret_TlsCertificate:
//	case *envoy_tls_v3.Secret_SessionTicketKeys:
//	case *envoy_tls_v3.Secret_ValidationContext:
//	case *envoy_tls_v3.Secret_GenericSecret:
//	}
//}
