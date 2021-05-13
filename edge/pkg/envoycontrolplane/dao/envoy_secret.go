package dao

type Secret struct {
	ID    string `orm:"column(id); size(64); pk"`
	Name  string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text);"`
}

func (secret *Secret) Type() string {
	return SecretType
}

func (secret *Secret) TableName() string {
	return SecretTableName
}

func (secret *Secret) GetID() string {
	return secret.ID
}

func (secret *Secret) GetName() string {
	return secret.Name
}

func (secret *Secret) GetValue() string {
	return secret.Value
}
