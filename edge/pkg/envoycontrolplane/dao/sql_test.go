package dao

import (
	"reflect"
	"testing"

	"github.com/astaxie/beego/orm"
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
)

func TestDeleteResource(t *testing.T) {
	orm.RegisterModel(new(Cluster))
	orm.RegisterModel(new(Endpoint))
	orm.RegisterModel(new(Listener))
	orm.RegisterModel(new(Router))
	orm.RegisterModel(new(Secret))
	dbm.InitDBConfig("sqlite3", "default", "/home/hoshino/edgecore.db")
	var resources = []DaoResource{
		&Secret{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Endpoint{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Cluster{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Router{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Listener{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
	}
	for _, daoResource := range resources {
		err := InsertOrUpdateResource(daoResource)
		if err != nil {
			t.Fatalf("failed to insert into database, err: %v", err)
		}
		err = DeleteResource(daoResource)
		if err != nil {
			t.Fatalf("failed to delete resource from database, err: %v", err)
		}
		daoResources, err := QueryResource(daoResource, "id", "test")
		if err != nil {
			t.Fatalf("failed to query resource from database, err: %v", err)
		}
		if len(*daoResources) != 0 {
			t.Fatalf("failed to delete resource from database, because the record still exists")
		}
	}
}

func TestUpdateResource(t *testing.T) {}

func TestInsertOrUpdateResource(t *testing.T) {
	orm.RegisterModel(new(Cluster))
	orm.RegisterModel(new(Endpoint))
	orm.RegisterModel(new(Listener))
	orm.RegisterModel(new(Router))
	orm.RegisterModel(new(Secret))
	dbm.InitDBConfig("sqlite3", "default", "/home/hoshino/edgecore.db")
	var resources = []DaoResource{
		&Secret{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Endpoint{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Cluster{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Router{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Listener{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
	}
	for _, daoResource := range resources {
		err := InsertOrUpdateResource(daoResource)
		if err != nil {
			t.Fatalf("failed to insert into database, err: %v", err)
		}
		daoResources, err := QueryResource(daoResource, "id", "test")
		if err != nil {
			t.Fatalf("failed to query resource from database, err: %v", err)
		}
		if len(*daoResources) != 1 || !reflect.DeepEqual((*daoResources)[0], daoResource) {
			t.Fatalf("failed to delete resource from database, because the record still exists")
		}
	}
}

func TestQueryResource(t *testing.T) {}

func TestQueryAllResources(t *testing.T) {
	orm.RegisterModel(new(Cluster))
	orm.RegisterModel(new(Endpoint))
	orm.RegisterModel(new(Listener))
	orm.RegisterModel(new(Router))
	orm.RegisterModel(new(Secret))
	dbm.InitDBConfig("sqlite3", "default", "/home/hoshino/edgecore.db")
	var resources = []DaoResource{
		&Secret{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Endpoint{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Cluster{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Router{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
		&Listener{
			ID:    "test",
			Name:  "test",
			Value: "testdeleteresource",
		},
	}
	for _, daoResource := range resources {
		err := InsertOrUpdateResource(daoResource)
		if err != nil {
			t.Fatalf("failed to insert into database, err: %v", err)
		}
		daoResources, err := QueryAllResources(daoResource)
		if err != nil {
			t.Fatalf("failed to query resource from database, err: %v", err)
		}
		if len(*daoResources) != 1 || !reflect.DeepEqual((*daoResources)[0], daoResource) {
			t.Fatalf("failed to delete resource from database, because the record still exists")
		}
	}
}
